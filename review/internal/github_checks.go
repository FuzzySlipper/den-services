package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
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
	runs, err := c.commitCheckRuns(ctx, repository, commitSHA)
	if err != nil {
		return GitHubCheckResult{}, err
	}
	return evaluateGitHubCheckRuns(runs, requiredChecks), nil
}

// CheckChange evaluates required checks for code containing query.CommitSHA.
// A check that is missing, cancelled, or failed on the commit itself may be
// satisfied by a later commit of query.Ref that contains it.
func (c *GitHubClient) CheckChange(ctx context.Context, query GitHubChangeQuery) (GitHubCheckResult, error) {
	if c.baseURL == "" {
		return GitHubCheckResult{}, NewServiceError(ErrGitHubChecksUnset, "github_checks_unconfigured", http.StatusInternalServerError)
	}
	own, err := c.commitCheckRuns(ctx, query.Repository, query.CommitSHA)
	if err != nil {
		return GitHubCheckResult{}, err
	}
	needed := checksNeedingLaterCommits(own, query.RequiredChecks)
	ref := strings.TrimSpace(query.Ref)
	if len(needed) == 0 || ref == "" || query.LaterCommitLimit <= 0 {
		return evaluateGitHubChangeRuns(own, nil, query.RequiredChecks), nil
	}
	laterCommits, err := c.laterCommitsOnRef(ctx, query.Repository, query.CommitSHA, ref)
	if err != nil {
		return GitHubCheckResult{}, err
	}
	branchRuns, actionsReadable, err := c.branchWorkflowRuns(ctx, query.Repository, ref)
	if err != nil {
		return GitHubCheckResult{}, err
	}
	candidates := laterCandidateCommits(laterCommits, branchRuns, actionsReadable, query.LaterCommitLimit)
	later := make([]commitCheckRuns, 0, len(candidates))
	for _, sha := range candidates {
		runs, err := c.commitCheckRuns(ctx, query.Repository, sha)
		if err != nil {
			return GitHubCheckResult{}, err
		}
		later = append(later, commitCheckRuns{SHA: sha, Runs: runs})
		if laterCommitsSatisfy(later, needed) {
			break
		}
	}
	result := evaluateGitHubChangeRuns(own, later, query.RequiredChecks)
	if result.Status == GitHubCheckGateStatusPending && len(result.MissingRequiredChecks) > 0 && result.AllObservedChecksTerminal &&
		workflowRunsActive(branchRuns, query.CommitSHA, laterCommits) {
		// A workflow run waiting in a concurrency group has no check runs yet,
		// so a missing name is not proof of a misnamed check while any run
		// for this change is still queued or running.
		result.AllObservedChecksTerminal = false
		result.Summary += ". Workflow runs for this change are still queued or running."
	}
	return result, nil
}

// branchWorkflowRuns reads the most recent Actions workflow runs on ref in
// one request. readable is false when the token cannot read Actions.
func (c *GitHubClient) branchWorkflowRuns(ctx context.Context, repository string, ref string) ([]githubWorkflowRunResponse, bool, error) {
	query := url.Values{"branch": []string{ref}, "per_page": []string{"100"}}
	var runs githubWorkflowRunsResponse
	if err := c.getJSON(ctx, "/repos/"+repository+"/actions/runs?"+query.Encode(), &runs); err != nil {
		var githubErr *GitHubHTTPError
		if errors.As(err, &githubErr) && ignorableCompareError(githubErr) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return runs.WorkflowRuns, true, nil
}

// workflowRunsActive reports whether a workflow run for the gated commit or
// any later containing commit has not completed.
func workflowRunsActive(runs []githubWorkflowRunResponse, commitSHA string, later []string) bool {
	wanted := make(map[string]struct{}, len(later)+1)
	wanted[strings.ToLower(commitSHA)] = struct{}{}
	for _, sha := range later {
		wanted[sha] = struct{}{}
	}
	for _, run := range runs {
		if _, ok := wanted[strings.ToLower(run.HeadSHA)]; ok && run.Status != "completed" {
			return true
		}
	}
	return false
}

// laterCandidateCommits chooses which later containing commits to read check
// runs for, nearest first, at most limit. With Actions readable it picks the
// commits that have a workflow run that passed or is still running, so a
// passing run is found however far along the ref it is, plus the ref head.
// Otherwise it falls back to the nearest commits plus the head.
func laterCandidateCommits(later []string, runs []githubWorkflowRunResponse, actionsReadable bool, limit int) []string {
	if limit <= 0 || len(later) == 0 {
		return nil
	}
	if !actionsReadable {
		return nearestAndHeadCommits(later, limit)
	}
	useful := make(map[string]struct{}, len(runs))
	for _, run := range runs {
		if run.Status != "completed" || successfulGitHubConclusion(run.Conclusion) {
			useful[strings.ToLower(run.HeadSHA)] = struct{}{}
		}
	}
	head := later[len(later)-1]
	candidates := make([]string, 0, limit)
	for _, sha := range later[:len(later)-1] {
		if len(candidates) == limit-1 {
			break
		}
		if _, ok := useful[sha]; ok {
			candidates = append(candidates, sha)
		}
	}
	return append(candidates, head)
}

// commitCheckRuns lists check runs for one exact commit, falling back to
// Actions jobs when the token cannot read the Checks API.
func (c *GitHubClient) commitCheckRuns(ctx context.Context, repository string, commitSHA string) ([]githubCheckRunResponse, error) {
	runs, err := c.commitCheckRunsFromChecksAPI(ctx, repository, commitSHA)
	if err != nil {
		var githubErr *GitHubHTTPError
		if !errors.As(err, &githubErr) || githubErr.Classification() != GitHubHTTPErrorPermissionDenied {
			return nil, err
		}
		runs, err = c.commitCheckRunsFromActions(ctx, repository, commitSHA)
		if err != nil {
			return nil, err
		}
	}
	for i := range runs {
		runs[i].HeadSHA = commitSHA
	}
	return runs, nil
}

func (c *GitHubClient) commitCheckRunsFromChecksAPI(ctx context.Context, repository string, commitSHA string) ([]githubCheckRunResponse, error) {
	var payload githubCheckRunsResponse
	requestPath := "/repos/" + repository + "/commits/" + url.PathEscape(commitSHA) + "/check-runs?per_page=100"
	if err := c.getJSON(ctx, requestPath, &payload); err != nil {
		return nil, err
	}
	return payload.CheckRuns, nil
}

func (c *GitHubClient) commitCheckRunsFromActions(ctx context.Context, repository string, commitSHA string) ([]githubCheckRunResponse, error) {
	query := url.Values{"head_sha": []string{commitSHA}, "per_page": []string{"100"}}
	var runs githubWorkflowRunsResponse
	if err := c.getJSON(ctx, "/repos/"+repository+"/actions/runs?"+query.Encode(), &runs); err != nil {
		return nil, err
	}
	checkRuns := make([]githubCheckRunResponse, 0)
	for _, run := range runs.WorkflowRuns {
		if run.HeadSHA != commitSHA {
			continue
		}
		var jobs githubWorkflowJobsResponse
		if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/actions/runs/%d/jobs?per_page=100", repository, run.ID), &jobs); err != nil {
			return nil, err
		}
		for _, job := range jobs.Jobs {
			checkRuns = append(checkRuns, githubCheckRunResponse{
				ID: job.ID, Name: job.Name, Status: job.Status, Conclusion: job.Conclusion, HTMLURL: job.HTMLURL,
				CreatedAt: job.CreatedAt, StartedAt: job.StartedAt, CompletedAt: job.CompletedAt,
			})
		}
	}
	return checkRuns, nil
}

// laterCommitsOnRef returns commits of ref that contain commitSHA, nearest
// first and ending at the ref head. It returns none when ref does not contain
// commitSHA, is identical to it, or cannot be compared.
func (c *GitHubClient) laterCommitsOnRef(ctx context.Context, repository string, commitSHA string, ref string) ([]string, error) {
	var payload githubCompareResponse
	basehead := url.PathEscape(commitSHA) + "..." + strings.ReplaceAll(url.PathEscape(ref), "%2F", "/")
	if err := c.getJSON(ctx, "/repos/"+repository+"/compare/"+basehead+"?per_page=100", &payload); err != nil {
		var githubErr *GitHubHTTPError
		if errors.As(err, &githubErr) && ignorableCompareError(githubErr) {
			return nil, nil
		}
		return nil, err
	}
	if payload.Status != "ahead" {
		return nil, nil
	}
	shas := make([]string, 0, len(payload.Commits))
	for _, commit := range payload.Commits {
		shas = append(shas, strings.ToLower(commit.SHA))
	}
	return shas, nil
}

func ignorableCompareError(err *GitHubHTTPError) bool {
	switch err.StatusCode {
	case http.StatusNotFound, http.StatusUnprocessableEntity:
		return true
	case http.StatusForbidden:
		return err.Classification() == GitHubHTTPErrorPermissionDenied
	default:
		return false
	}
}

func nearestAndHeadCommits(commits []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	if len(commits) <= limit {
		return append([]string(nil), commits...)
	}
	shas := append([]string(nil), commits[:limit-1]...)
	return append(shas, commits[len(commits)-1])
}

func (c *GitHubClient) getJSON(ctx context.Context, requestPath string, target any) error {
	requestURL := c.baseURL + requestPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("building github API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("requesting github API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return fmt.Errorf("reading github API error response: %w", readErr)
		}
		return newGitHubHTTPError(resp, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decoding github API response: %w", err)
	}
	return nil
}

type GitHubHTTPErrorClassification string

const (
	GitHubHTTPErrorPermissionDenied   GitHubHTTPErrorClassification = "permission_denied"
	GitHubHTTPErrorPrimaryRateLimit   GitHubHTTPErrorClassification = "primary_rate_limit"
	GitHubHTTPErrorSecondaryRateLimit GitHubHTTPErrorClassification = "secondary_rate_limit"
	GitHubHTTPErrorOther              GitHubHTTPErrorClassification = "http_error"
)

type GitHubHTTPError struct {
	Status                string
	StatusCode            int
	Message               string
	RateLimitRemaining    int
	RateLimitRemainingSet bool
	RateLimitReset        time.Time
	RateLimitResetSet     bool
	RetryAfter            time.Duration
	RetryAfterSet         bool
	RequestID             string
}

func (e *GitHubHTTPError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return fmt.Sprintf("github checks request failed: %s: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("github checks request failed: %s", e.Status)
}

func (e *GitHubHTTPError) Classification() GitHubHTTPErrorClassification {
	if e.RateLimitRemainingSet && e.RateLimitRemaining == 0 {
		return GitHubHTTPErrorPrimaryRateLimit
	}
	message := strings.ToLower(strings.TrimSpace(e.Message))
	if (e.StatusCode == http.StatusForbidden || e.StatusCode == http.StatusTooManyRequests) &&
		(e.RetryAfterSet || strings.Contains(message, "secondary rate limit") || strings.Contains(message, "abuse detection")) {
		return GitHubHTTPErrorSecondaryRateLimit
	}
	if e.StatusCode == http.StatusTooManyRequests {
		return GitHubHTTPErrorSecondaryRateLimit
	}
	if e.StatusCode == http.StatusForbidden {
		return GitHubHTTPErrorPermissionDenied
	}
	return GitHubHTTPErrorOther
}

func newGitHubHTTPError(resp *http.Response, body []byte) *GitHubHTTPError {
	var payload struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &payload)
	err := &GitHubHTTPError{
		Status:     resp.Status,
		StatusCode: resp.StatusCode,
		Message:    strings.TrimSpace(payload.Message),
		RequestID:  strings.TrimSpace(resp.Header.Get("x-github-request-id")),
	}
	if remaining, parseErr := strconv.Atoi(strings.TrimSpace(resp.Header.Get("x-ratelimit-remaining"))); parseErr == nil {
		err.RateLimitRemaining = remaining
		err.RateLimitRemainingSet = true
	}
	if resetUnix, parseErr := strconv.ParseInt(strings.TrimSpace(resp.Header.Get("x-ratelimit-reset")), 10, 64); parseErr == nil {
		err.RateLimitReset = time.Unix(resetUnix, 0).UTC()
		err.RateLimitResetSet = true
	}
	if retryAfter, ok := parseGitHubRetryAfter(resp.Header.Get("retry-after")); ok {
		err.RetryAfter = retryAfter
		err.RetryAfterSet = true
	}
	return err
}

func parseGitHubRetryAfter(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := time.Until(when)
	if delay <= 0 {
		return 0, false
	}
	return delay, true
}

type githubCheckRunsResponse struct {
	CheckRuns []githubCheckRunResponse `json:"check_runs"`
}

type githubWorkflowRunsResponse struct {
	WorkflowRuns []githubWorkflowRunResponse `json:"workflow_runs"`
}

type githubWorkflowRunResponse struct {
	ID         int64  `json:"id"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type githubWorkflowJobsResponse struct {
	Jobs []githubWorkflowJobResponse `json:"jobs"`
}

type githubWorkflowJobResponse struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	HTMLURL     string     `json:"html_url"`
	CreatedAt   *time.Time `json:"created_at"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

type githubCompareResponse struct {
	Status  string                `json:"status"`
	Commits []githubCompareCommit `json:"commits"`
}

type githubCompareCommit struct {
	SHA string `json:"sha"`
}

type githubCheckRunResponse struct {
	ID          int64      `json:"id"`
	HeadSHA     string     `json:"head_sha"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	HTMLURL     string     `json:"html_url"`
	DetailsURL  string     `json:"details_url"`
	CreatedAt   *time.Time `json:"created_at"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Output      struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	} `json:"output"`
}

type commitCheckRuns struct {
	SHA  string
	Runs []githubCheckRunResponse
}

// checksNeedingLaterCommits names required checks the commit itself has not
// passed and is not still running: missing, cancelled, or failed.
func checksNeedingLaterCommits(own []githubCheckRunResponse, requiredChecks []string) []string {
	latest := latestGitHubCheckRunsByName(own)
	var needed []string
	for _, name := range trimSlice(requiredChecks) {
		run, ok := latest[name]
		if ok && (run.Status != "completed" || successfulGitHubConclusion(run.Conclusion)) {
			continue
		}
		needed = append(needed, name)
	}
	return needed
}

func laterCommitsSatisfy(later []commitCheckRuns, needed []string) bool {
	latest := latestRunsPerCommit(later)
	for _, name := range needed {
		if _, ok := firstLaterSuccess(latest, name); !ok {
			return false
		}
	}
	return true
}

// evaluateGitHubCheckRuns decides required checks from one commit's runs.
func evaluateGitHubCheckRuns(runs []githubCheckRunResponse, requiredChecks []string) GitHubCheckResult {
	return evaluateGitHubChangeRuns(runs, nil, requiredChecks)
}

// evaluateGitHubChangeRuns decides a gate from the gated commit's own runs and
// runs on later commits that contain it (nearest first). A success on either
// satisfies a check. A cancelled run is treated as missing. A failure on a
// later commit keeps the check pending because a later change may have caused
// it; only a failure on the gated commit, with no later run passing or still
// running, fails the gate.
func evaluateGitHubChangeRuns(own []githubCheckRunResponse, later []commitCheckRuns, requiredChecks []string) GitHubCheckResult {
	ownLatest := latestGitHubCheckRunsByName(own)
	laterLatest := latestRunsPerCommit(later)
	observedRuns := make([]GitHubCheckRun, 0, len(ownLatest))
	allObservedTerminal := len(ownLatest) > 0
	for _, run := range ownLatest {
		observedRuns = append(observedRuns, convertGitHubCheckRun(run))
		if run.Status != "completed" {
			allObservedTerminal = false
		}
	}
	sort.Slice(observedRuns, func(i int, j int) bool { return observedRuns[i].Name < observedRuns[j].Name })
	var missing, pending, failed []string
	var resultRuns []GitHubCheckRun
	fromLaterCommit := false
	for _, name := range trimSlice(requiredChecks) {
		run, ok := ownLatest[name]
		if ok && (run.Status != "completed" || successfulGitHubConclusion(run.Conclusion)) {
			resultRuns = append(resultRuns, convertGitHubCheckRun(run))
			if run.Status != "completed" {
				pending = append(pending, name)
			}
			continue
		}
		if laterRun, found := firstLaterSuccess(laterLatest, name); found {
			resultRuns = append(resultRuns, convertGitHubCheckRun(laterRun))
			fromLaterCommit = true
			continue
		}
		laterRunning, laterSeen := laterRunState(laterLatest, name)
		switch {
		case laterRunning != nil:
			pending = append(pending, name)
			resultRuns = append(resultRuns, convertGitHubCheckRun(*laterRunning))
		case ok && run.Conclusion != "cancelled":
			failed = append(failed, fmt.Sprintf("%s (%s)", name, firstNonEmpty(run.Conclusion, "unknown")))
			resultRuns = append(resultRuns, convertGitHubCheckRun(run))
		case ok:
			pending = append(pending, name)
			resultRuns = append(resultRuns, convertGitHubCheckRun(run))
		case laterSeen != nil:
			pending = append(pending, name)
			resultRuns = append(resultRuns, convertGitHubCheckRun(*laterSeen))
		default:
			missing = append(missing, name)
			resultRuns = append(resultRuns, GitHubCheckRun{Name: name, Status: GitHubCheckGateStatusPending})
		}
	}
	sort.Slice(resultRuns, func(i int, j int) bool { return resultRuns[i].Name < resultRuns[j].Name })
	if len(failed) > 0 {
		return GitHubCheckResult{
			Status: GitHubCheckGateStatusFailed, CheckRuns: resultRuns,
			Summary:           "One or more required GitHub checks failed.",
			FailureSummary:    "Failed checks: " + strings.Join(failed, ", "),
			TerminalReason:    GitHubCheckTerminalReasonChecksFailed,
			ObservedCheckRuns: observedRuns, MissingRequiredChecks: missing,
			AllObservedChecksTerminal: allObservedTerminal,
		}
	}
	if len(missing) > 0 || len(pending) > 0 {
		waiting := append([]string{}, missing...)
		waiting = append(waiting, pending...)
		summary := "Waiting for required checks: " + strings.Join(waiting, ", ")
		if len(missing) > 0 && len(observedRuns) > 0 {
			summary += ". Observed check runs: " + strings.Join(githubCheckRunNames(observedRuns), ", ")
		}
		return GitHubCheckResult{
			Status: GitHubCheckGateStatusPending, CheckRuns: resultRuns,
			Summary: summary, ObservedCheckRuns: observedRuns, MissingRequiredChecks: missing,
			AllObservedChecksTerminal: allObservedTerminal,
		}
	}
	summary := "All required GitHub checks passed."
	if fromLaterCommit {
		summary = "All required GitHub checks passed; some passed on a later commit that contains this commit."
	}
	return GitHubCheckResult{
		Status: GitHubCheckGateStatusPassed, CheckRuns: resultRuns,
		Summary: summary, TerminalReason: GitHubCheckTerminalReasonChecksPassed,
		ObservedCheckRuns: observedRuns, AllObservedChecksTerminal: allObservedTerminal,
	}
}

func latestGitHubCheckRunsByName(runs []githubCheckRunResponse) map[string]githubCheckRunResponse {
	latestByName := make(map[string]githubCheckRunResponse, len(runs))
	for _, run := range runs {
		existing, ok := latestByName[run.Name]
		if !ok || run.ID > existing.ID {
			latestByName[run.Name] = run
		}
	}
	return latestByName
}

func latestRunsPerCommit(commits []commitCheckRuns) []map[string]githubCheckRunResponse {
	latest := make([]map[string]githubCheckRunResponse, 0, len(commits))
	for _, commit := range commits {
		latest = append(latest, latestGitHubCheckRunsByName(commit.Runs))
	}
	return latest
}

func firstLaterSuccess(later []map[string]githubCheckRunResponse, name string) (githubCheckRunResponse, bool) {
	for _, runs := range later {
		if run, ok := runs[name]; ok && run.Status == "completed" && successfulGitHubConclusion(run.Conclusion) {
			return run, true
		}
	}
	return githubCheckRunResponse{}, false
}

// laterRunState returns the nearest later run still in progress, and the
// nearest later run of any state.
func laterRunState(later []map[string]githubCheckRunResponse, name string) (*githubCheckRunResponse, *githubCheckRunResponse) {
	var running, seen *githubCheckRunResponse
	for _, runs := range later {
		run, ok := runs[name]
		if !ok {
			continue
		}
		if seen == nil {
			seen = &run
		}
		if run.Status != "completed" && running == nil {
			running = &run
		}
	}
	return running, seen
}

func convertGitHubCheckRun(run githubCheckRunResponse) GitHubCheckRun {
	return GitHubCheckRun{
		Name: run.Name, HeadSHA: strings.ToLower(run.HeadSHA), Status: run.Status, Conclusion: run.Conclusion,
		URL: run.HTMLURL, DetailsURL: run.DetailsURL, Summary: firstNonEmpty(run.Output.Title, run.Output.Summary),
		CreatedAt: run.CreatedAt, StartedAt: run.StartedAt, CompletedAt: run.CompletedAt,
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
	scanInterval time.Duration
	batchSize    int
	logger       *slog.Logger
}

func NewGitHubCheckWatcher(service *Service, scanInterval time.Duration, batchSize int, logger *slog.Logger) *GitHubCheckWatcher {
	if scanInterval <= 0 {
		scanInterval = 5 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 10
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &GitHubCheckWatcher{service: service, scanInterval: scanInterval, batchSize: batchSize, logger: logger}
}

func (w *GitHubCheckWatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.scanInterval)
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
		Status:                gate.Status,
		Summary:               gate.Summary,
		FailureSummary:        gate.FailureSummary,
		CheckRuns:             gate.CheckRuns,
		ObservedCheckRuns:     gate.ObservedCheckRuns,
		MissingRequiredChecks: gate.MissingRequiredChecks,
		TerminalReason:        gate.TerminalReason,
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
	if result.TerminalReason != "" {
		fmt.Fprintf(&b, "Reason: `%s`\n\n", result.TerminalReason)
	}
	if len(result.MissingRequiredChecks) > 0 {
		fmt.Fprintf(&b, "Missing required checks: %s\n\n", strings.Join(result.MissingRequiredChecks, ", "))
	}
	appendCheckRunLinks(&b, result.CheckRuns, gate.CommitSHA)
	if len(result.MissingRequiredChecks) > 0 && len(result.ObservedCheckRuns) > 0 {
		b.WriteString("\nObserved check runs:\n")
		appendNamedCheckRunLinks(&b, result.ObservedCheckRuns)
	}
	return strings.TrimSpace(b.String())
}

func appendCheckRunLinks(b *strings.Builder, runs []GitHubCheckRun, gateCommitSHA string) {
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
		if run.HeadSHA != "" && run.HeadSHA != gateCommitSHA {
			state += " on later commit `" + run.HeadSHA + "`"
		}
		if link != "" {
			fmt.Fprintf(b, "- %s: %s (%s)\n", run.Name, state, link)
		} else {
			fmt.Fprintf(b, "- %s: %s\n", run.Name, state)
		}
	}
}

func appendNamedCheckRunLinks(b *strings.Builder, runs []GitHubCheckRun) {
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
