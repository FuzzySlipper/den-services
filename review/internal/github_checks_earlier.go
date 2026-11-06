package review

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// compareFileLimit is the most files GitHub lists for one comparison. A list
// that long may be cut short, so it proves nothing.
const compareFileLimit = 300

// earlierPathFilteredPasses finds passes for required checks that never ran on
// the gated commit because a paths filter skipped their workflow. For each
// workflow it takes the nearest run on the ref before the gated commit; a
// missing check in that run counts only when the job succeeded and
// workflowSkippedSince proves nothing since then would have triggered it. A
// workflow that ran on the gated commit is not skipped, so it never counts.
func (c *GitHubClient) earlierPathFilteredPasses(ctx context.Context, query GitHubChangeQuery, laterCommits []string, missing []string) (map[string]githubCheckRunResponse, error) {
	runs, readable, err := c.branchWorkflowRuns(ctx, query.Repository, strings.TrimSpace(query.Ref), "")
	if err != nil || !readable {
		return nil, err
	}
	passes := make(map[string]githubCheckRunResponse, len(missing))
	for _, run := range nearestEarlierWorkflowRuns(runs, query.CommitSHA, laterCommits) {
		jobs, err := c.workflowRunJobs(ctx, query.Repository, run.ID)
		if err != nil {
			return nil, err
		}
		latest := latestGitHubCheckRunsByName(jobs)
		var found []githubCheckRunResponse
		for _, name := range missing {
			if job, ok := latest[name]; ok && job.Status == "completed" && job.Conclusion == "success" {
				found = append(found, job)
			}
		}
		if len(found) == 0 {
			continue
		}
		skipped, err := c.workflowSkippedSince(ctx, query.Repository, run.Path, run.HeadSHA, query.CommitSHA)
		if err != nil {
			return nil, err
		}
		if !skipped {
			continue
		}
		for _, job := range found {
			job.HeadSHA = run.HeadSHA
			passes[job.Name] = job
		}
	}
	return passes, nil
}

// nearestEarlierWorkflowRuns keeps the newest push or manual run of each
// workflow older than the gated commit. runs are newest first.
func nearestEarlierWorkflowRuns(runs []githubWorkflowRunResponse, commitSHA string, later []string) []githubWorkflowRunResponse {
	commitSHA = strings.ToLower(commitSHA)
	isLater := make(map[string]struct{}, len(later))
	for _, sha := range later {
		isLater[sha] = struct{}{}
	}
	decided := make(map[string]struct{}, len(runs))
	var nearest []githubWorkflowRunResponse
	for _, run := range runs {
		sha := strings.ToLower(run.HeadSHA)
		if _, ok := isLater[sha]; ok {
			continue
		}
		if _, ok := decided[run.Path]; ok {
			continue
		}
		if sha == commitSHA {
			decided[run.Path] = struct{}{}
			continue
		}
		if run.Event != "push" && run.Event != "workflow_dispatch" {
			continue
		}
		decided[run.Path] = struct{}{}
		nearest = append(nearest, run)
	}
	return nearest
}

// workflowSkippedSince reports whether the workflow's push paths filter, as it
// stands at commitSHA, skips every file changed since earlierSHA. Whatever
// Review cannot read or fully understand answers false, keeping the gate
// strict. A change to the workflow itself also answers false, because the
// earlier run tested a different workflow.
func (c *GitHubClient) workflowSkippedSince(ctx context.Context, repository string, workflowPath string, earlierSHA string, commitSHA string) (bool, error) {
	content, ok, err := c.fileAtCommit(ctx, repository, workflowPath, commitSHA)
	if err != nil || !ok {
		return false, err
	}
	filter, ok := parsePushPathFilter(content)
	if !ok {
		return false, nil
	}
	files, ok, err := c.filesChangedSince(ctx, repository, earlierSHA, commitSHA)
	if err != nil || !ok {
		return false, err
	}
	for _, file := range files {
		if file == workflowPath || filter.triggeredBy(file) {
			return false, nil
		}
	}
	return true, nil
}

func (c *GitHubClient) fileAtCommit(ctx context.Context, repository string, path string, commitSHA string) ([]byte, bool, error) {
	var payload struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	requestPath := "/repos/" + repository + "/contents/" + path + "?" + url.Values{"ref": []string{commitSHA}}.Encode()
	if err := c.getJSON(ctx, requestPath, &payload); err != nil {
		var githubErr *GitHubHTTPError
		if errors.As(err, &githubErr) && ignorableCompareError(githubErr) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if payload.Encoding != "base64" {
		return nil, false, nil
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(payload.Content, "\n", ""))
	if err != nil {
		return nil, false, nil
	}
	return content, true, nil
}

// filesChangedSince lists the files changed from earlierSHA to commitSHA,
// including the old name of a renamed file. ok is false unless earlierSHA is an
// ancestor of commitSHA and the list is complete.
func (c *GitHubClient) filesChangedSince(ctx context.Context, repository string, earlierSHA string, commitSHA string) ([]string, bool, error) {
	var payload githubCompareResponse
	requestPath := "/repos/" + repository + "/compare/" + url.PathEscape(earlierSHA) + "..." + url.PathEscape(commitSHA)
	if err := c.getJSON(ctx, requestPath, &payload); err != nil {
		var githubErr *GitHubHTTPError
		if errors.As(err, &githubErr) && ignorableCompareError(githubErr) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if payload.Status != "ahead" || len(payload.Files) >= compareFileLimit {
		return nil, false, nil
	}
	files := make([]string, 0, len(payload.Files))
	for _, file := range payload.Files {
		files = append(files, file.Filename)
		if file.PreviousFilename != "" {
			files = append(files, file.PreviousFilename)
		}
	}
	return files, true, nil
}

func githubRunsCompleted(runs []githubCheckRunResponse) bool {
	for _, run := range latestGitHubCheckRunsByName(runs) {
		if run.Status != "completed" {
			return false
		}
	}
	return true
}

// pushPathFilter is a workflow's on.push paths or paths-ignore filter.
type pushPathFilter struct {
	patterns []pathPattern
	ignore   bool
}

type pathPattern struct {
	negated bool
	match   *regexp.Regexp
}

// parsePushPathFilter reads the filter from a workflow file. ok is false when
// the workflow has no such filter or uses a pattern Review does not interpret.
func parsePushPathFilter(workflow []byte) (pushPathFilter, bool) {
	var file struct {
		On yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal(workflow, &file); err != nil || file.On.Kind != yaml.MappingNode {
		return pushPathFilter{}, false
	}
	var triggers struct {
		Push struct {
			Paths       []string `yaml:"paths"`
			PathsIgnore []string `yaml:"paths-ignore"`
		} `yaml:"push"`
	}
	if err := file.On.Decode(&triggers); err != nil {
		return pushPathFilter{}, false
	}
	paths, ignored := triggers.Push.Paths, triggers.Push.PathsIgnore
	if (len(paths) == 0) == (len(ignored) == 0) {
		return pushPathFilter{}, false
	}
	filter := pushPathFilter{ignore: len(ignored) > 0}
	for _, raw := range append(paths, ignored...) {
		pattern, ok := compilePathPattern(raw)
		if !ok {
			return pushPathFilter{}, false
		}
		filter.patterns = append(filter.patterns, pattern)
	}
	return filter, true
}

// compilePathPattern handles the GitHub filter syntax of *, ** and a leading !.
// The rarer ?, +, [] and \ are refused rather than guessed at.
func compilePathPattern(raw string) (pathPattern, bool) {
	glob, negated := strings.CutPrefix(raw, "!")
	if glob == "" || strings.HasSuffix(glob, "/") || strings.ContainsAny(glob, `?+[]\`) {
		return pathPattern{}, false
	}
	var expression strings.Builder
	expression.WriteString("^")
	for index := 0; index < len(glob); {
		rest := glob[index:]
		switch {
		case strings.HasPrefix(rest, "**/"):
			expression.WriteString("(?:.*/)?")
			index += 3
		case strings.HasPrefix(rest, "**"):
			expression.WriteString(".*")
			index += 2
		case rest[0] == '*':
			expression.WriteString("[^/]*")
			index++
		default:
			expression.WriteString(regexp.QuoteMeta(rest[:1]))
			index++
		}
	}
	expression.WriteString("$")
	match, err := regexp.Compile(expression.String())
	if err != nil {
		return pathPattern{}, false
	}
	return pathPattern{negated: negated, match: match}, true
}

// triggeredBy reports whether a change to file would run the workflow. As on
// GitHub, the last matching pattern decides, so a later ! can exclude a file.
func (f pushPathFilter) triggeredBy(file string) bool {
	matched := false
	for _, pattern := range f.patterns {
		if pattern.match.MatchString(file) {
			matched = !pattern.negated
		}
	}
	return matched != f.ignore
}
