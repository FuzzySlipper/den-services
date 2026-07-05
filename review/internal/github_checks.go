package review

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type GitHubClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewGitHubClient(baseURL string, token string, timeout time.Duration) *GitHubClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &GitHubClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		client:  &http.Client{Timeout: timeout},
	}
}

func (c *GitHubClient) CheckCommit(ctx context.Context, repository string, commitSHA string, requiredChecks []string) (GitHubCheckResult, error) {
	if c.baseURL == "" {
		return GitHubCheckResult{}, NewServiceError(ErrGitHubChecksUnset, "github_checks_unconfigured", http.StatusInternalServerError)
	}
	requestURL := c.baseURL + "/repos/" + repository + "/commits/" + url.PathEscape(commitSHA) + "/check-runs?per_page=100"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return GitHubCheckResult{}, fmt.Errorf("building github checks request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return GitHubCheckResult{}, fmt.Errorf("requesting github checks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return GitHubCheckResult{}, fmt.Errorf("github checks request failed: %s", resp.Status)
	}
	var payload githubCheckRunsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return GitHubCheckResult{}, fmt.Errorf("decoding github checks: %w", err)
	}
	return evaluateGitHubCheckRuns(payload.CheckRuns, requiredChecks), nil
}

type githubCheckRunsResponse struct {
	CheckRuns []githubCheckRunResponse `json:"check_runs"`
}

type githubCheckRunResponse struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	DetailsURL string `json:"details_url"`
	Output     struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	} `json:"output"`
}

func evaluateGitHubCheckRuns(runs []githubCheckRunResponse, requiredChecks []string) GitHubCheckResult {
	latestByName := make(map[string]githubCheckRunResponse, len(runs))
	for _, run := range runs {
		existing, ok := latestByName[run.Name]
		if !ok || run.ID > existing.ID {
			latestByName[run.Name] = run
		}
	}
	var missing []string
	var pending []string
	var failed []string
	var resultRuns []GitHubCheckRun
	for _, name := range trimSlice(requiredChecks) {
		run, ok := latestByName[name]
		if !ok {
			missing = append(missing, name)
			resultRuns = append(resultRuns, GitHubCheckRun{Name: name, Status: GitHubCheckGateStatusPending})
			continue
		}
		converted := GitHubCheckRun{
			Name: run.Name, Status: run.Status, Conclusion: run.Conclusion,
			URL: run.HTMLURL, DetailsURL: run.DetailsURL, Summary: firstNonEmpty(run.Output.Title, run.Output.Summary),
		}
		resultRuns = append(resultRuns, converted)
		if run.Status != "completed" {
			pending = append(pending, name)
			continue
		}
		if !successfulGitHubConclusion(run.Conclusion) {
			failed = append(failed, fmt.Sprintf("%s (%s)", name, firstNonEmpty(run.Conclusion, "unknown")))
		}
	}
	sort.Slice(resultRuns, func(i int, j int) bool { return resultRuns[i].Name < resultRuns[j].Name })
	if len(failed) > 0 {
		return GitHubCheckResult{
			Status: GitHubCheckGateStatusFailed, CheckRuns: resultRuns,
			Summary:        "One or more required GitHub checks failed.",
			FailureSummary: "Failed checks: " + strings.Join(failed, ", "),
		}
	}
	if len(missing) > 0 || len(pending) > 0 {
		waiting := append([]string{}, missing...)
		waiting = append(waiting, pending...)
		return GitHubCheckResult{
			Status: GitHubCheckGateStatusPending, CheckRuns: resultRuns,
			Summary: "Waiting for required checks: " + strings.Join(waiting, ", "),
		}
	}
	return GitHubCheckResult{
		Status: GitHubCheckGateStatusPassed, CheckRuns: resultRuns,
		Summary: "All required GitHub checks passed.",
	}
}

func successfulGitHubConclusion(conclusion string) bool {
	switch conclusion {
	case "success", "neutral", "skipped":
		return true
	default:
		return false
	}
}

type GitHubCheckWatcher struct {
	service      *Service
	pollInterval time.Duration
	batchSize    int
	logger       *slog.Logger
}

func NewGitHubCheckWatcher(service *Service, pollInterval time.Duration, batchSize int, logger *slog.Logger) *GitHubCheckWatcher {
	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 10
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &GitHubCheckWatcher{service: service, pollInterval: pollInterval, batchSize: batchSize, logger: logger}
}

func (w *GitHubCheckWatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	w.poll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

func (w *GitHubCheckWatcher) poll(ctx context.Context) {
	if err := w.service.PollGitHubCheckGates(ctx, w.batchSize); err != nil {
		w.logger.Warn("polling github check gates", "error", err)
	}
}

func renderGitHubCheckGateEvidence(gate *GitHubCheckGate) (string, string) {
	result := GitHubCheckResult{
		Status:         gate.Status,
		Summary:        gate.Summary,
		FailureSummary: gate.FailureSummary,
		CheckRuns:      gate.CheckRuns,
	}
	switch gate.Status {
	case GitHubCheckGateStatusPassed:
		return renderGitHubCheckGateMessage(gate, result), "github_checks_passed"
	case GitHubCheckGateStatusFailed:
		return renderGitHubCheckGateMessage(gate, result), "github_checks_failed"
	case GitHubCheckGateStatusTimedOut:
		return renderGitHubCheckGateMessage(gate, result), "github_checks_timeout"
	case GitHubCheckGateStatusSuperseded:
		return renderGitHubCheckGateMessage(gate, result), "github_checks_superseded"
	default:
		return renderGitHubCheckGateMessage(gate, result), "github_checks_updated"
	}
}

func renderGitHubCheckGateMessage(gate *GitHubCheckGate, result GitHubCheckResult) string {
	var b strings.Builder
	switch result.Status {
	case GitHubCheckGateStatusPassed:
		fmt.Fprintf(&b, "GitHub checks passed for `%s` on `%s`.\n\n", gate.CommitSHA, gate.Ref)
	case GitHubCheckGateStatusFailed:
		fmt.Fprintf(&b, "GitHub checks failed for `%s` on `%s`.\n\n", gate.CommitSHA, gate.Ref)
	case GitHubCheckGateStatusTimedOut:
		fmt.Fprintf(&b, "GitHub checks timed out for `%s` on `%s`.\n\n", gate.CommitSHA, gate.Ref)
	case GitHubCheckGateStatusSuperseded:
		fmt.Fprintf(&b, "GitHub check gate superseded for `%s` on `%s`.\n\n", gate.CommitSHA, gate.Ref)
	default:
		fmt.Fprintf(&b, "GitHub checks updated for `%s` on `%s`.\n\n", gate.CommitSHA, gate.Ref)
	}
	if result.FailureSummary != "" {
		fmt.Fprintf(&b, "%s\n\n", result.FailureSummary)
	} else if result.Summary != "" {
		fmt.Fprintf(&b, "%s\n\n", result.Summary)
	}
	appendCheckRunLinks(&b, result.CheckRuns)
	return strings.TrimSpace(b.String())
}

func appendCheckRunLinks(b *strings.Builder, runs []GitHubCheckRun) {
	if len(runs) == 0 {
		return
	}
	b.WriteString("Check runs:\n")
	for _, run := range runs {
		link := firstNonEmpty(run.URL, run.DetailsURL)
		state := strings.TrimSpace(run.Status)
		if run.Conclusion != "" {
			state += "/" + run.Conclusion
		}
		if link != "" {
			fmt.Fprintf(b, "- %s: %s (%s)\n", run.Name, state, link)
		} else {
			fmt.Fprintf(b, "- %s: %s\n", run.Name, state)
		}
	}
}
