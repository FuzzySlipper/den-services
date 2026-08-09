package review

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGitHubCheckWatcherUsesIndependentScanInterval(t *testing.T) {
	watcher := NewGitHubCheckWatcher(nil, 5*time.Second, 10, slog.Default())
	if watcher.scanInterval != 5*time.Second {
		t.Fatalf("scan interval = %s", watcher.scanInterval)
	}
}

func TestGitHubClientReturnsHTTPErrorDetails(t *testing.T) {
	resetAt := time.Date(2026, 7, 6, 12, 30, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.Header().Set("x-ratelimit-remaining", "0")
		w.Header().Set("x-ratelimit-reset", "1783341000")
		w.Header().Set("retry-after", "120")
		w.Header().Set("x-github-request-id", "request-1")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer server.Close()
	client := NewGitHubClient(server.URL, "", time.Second)

	_, err := client.CheckCommit(context.Background(), "owner/repo", "0123456789abcdef0123456789abcdef01234567", []string{"Verify Offline"})
	if err == nil {
		t.Fatal("expected GitHub HTTP error")
	}
	var githubErr *GitHubHTTPError
	if !errors.As(err, &githubErr) {
		t.Fatalf("error was not GitHubHTTPError: %T %v", err, err)
	}
	if githubErr.StatusCode != http.StatusForbidden || githubErr.Message != "API rate limit exceeded" {
		t.Fatalf("unexpected GitHub error details: %+v", githubErr)
	}
	if !githubErr.RateLimitRemainingSet || githubErr.RateLimitRemaining != 0 {
		t.Fatalf("rate remaining header not parsed: %+v", githubErr)
	}
	if !githubErr.RateLimitResetSet || !githubErr.RateLimitReset.Equal(resetAt) {
		t.Fatalf("rate reset header not parsed: %+v", githubErr)
	}
	if !githubErr.RetryAfterSet || githubErr.RetryAfter != 2*time.Minute {
		t.Fatalf("retry-after header not parsed: %+v", githubErr)
	}
	if githubErr.RequestID != "request-1" {
		t.Fatalf("request id not parsed: %+v", githubErr)
	}
	if got := githubErr.Classification(); got != GitHubHTTPErrorPrimaryRateLimit {
		t.Fatalf("classification = %q, want %q", got, GitHubHTTPErrorPrimaryRateLimit)
	}
}

func TestGitHubHTTPErrorClassification(t *testing.T) {
	tests := []struct {
		name string
		err  GitHubHTTPError
		want GitHubHTTPErrorClassification
	}{
		{
			name: "permission denial with quota remaining",
			err: GitHubHTTPError{
				StatusCode: http.StatusForbidden, Message: "Resource not accessible by personal access token",
				RateLimitRemaining: 4971, RateLimitRemainingSet: true,
			},
			want: GitHubHTTPErrorPermissionDenied,
		},
		{
			name: "primary rate limit",
			err: GitHubHTTPError{
				StatusCode: http.StatusForbidden, Message: "API rate limit exceeded",
				RateLimitRemaining: 0, RateLimitRemainingSet: true,
			},
			want: GitHubHTTPErrorPrimaryRateLimit,
		},
		{
			name: "secondary rate limit",
			err:  GitHubHTTPError{StatusCode: http.StatusForbidden, Message: "You have exceeded a secondary rate limit", RetryAfter: time.Minute, RetryAfterSet: true},
			want: GitHubHTTPErrorSecondaryRateLimit,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.err.Classification(); got != test.want {
				t.Fatalf("Classification() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestGitHubClientFallsBackToActionsWhenChecksPermissionDenied(t *testing.T) {
	const commitSHA = "0123456789abcdef0123456789abcdef01234567"
	var checkRunsCalls, workflowRunsCalls, jobsCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/repo/commits/" + commitSHA + "/check-runs":
			checkRunsCalls++
			w.Header().Set("x-ratelimit-remaining", "4971")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Resource not accessible by personal access token"}`))
		case "/repos/owner/repo/actions/runs":
			workflowRunsCalls++
			if got := r.URL.Query().Get("head_sha"); got != commitSHA {
				t.Errorf("head_sha = %q, want %q", got, commitSHA)
			}
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":123,"head_sha":"` + commitSHA + `"}]}`))
		case "/repos/owner/repo/actions/runs/123/jobs":
			jobsCalls++
			_, _ = w.Write([]byte(`{"jobs":[{"id":456,"name":"ci","status":"completed","conclusion":"failure","html_url":"https://github.test/job/456","started_at":"2026-08-09T05:00:32Z","completed_at":"2026-08-09T05:04:10Z"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := NewGitHubClient(server.URL, "token", time.Second).CheckCommit(context.Background(), "owner/repo", commitSHA, []string{"ci"})
	if err != nil {
		t.Fatalf("CheckCommit() error = %v", err)
	}
	if result.Status != GitHubCheckGateStatusFailed || result.TerminalReason != GitHubCheckTerminalReasonChecksFailed {
		t.Fatalf("result = %+v", result)
	}
	if len(result.CheckRuns) != 1 || result.CheckRuns[0].Name != "ci" || result.CheckRuns[0].Conclusion != "failure" {
		t.Fatalf("check runs = %+v", result.CheckRuns)
	}
	if checkRunsCalls != 1 || workflowRunsCalls != 1 || jobsCalls != 1 {
		t.Fatalf("calls: checks=%d workflows=%d jobs=%d", checkRunsCalls, workflowRunsCalls, jobsCalls)
	}
}

func TestGitHubClientDoesNotFallbackToActionsOnPrimaryRateLimit(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("content-type", "application/json")
		w.Header().Set("x-ratelimit-remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer server.Close()

	_, err := NewGitHubClient(server.URL, "token", time.Second).CheckCommit(context.Background(), "owner/repo", "0123456789abcdef0123456789abcdef01234567", []string{"ci"})
	var githubErr *GitHubHTTPError
	if !errors.As(err, &githubErr) || githubErr.Classification() != GitHubHTTPErrorPrimaryRateLimit {
		t.Fatalf("error = %T %v", err, err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestEvaluateGitHubCheckRunsReportsMissingAndObservedNames(t *testing.T) {
	result := evaluateGitHubCheckRuns([]githubCheckRunResponse{
		{ID: 10, Name: "Verify Offline", Status: "completed", Conclusion: "success", HTMLURL: "https://github.test/offline"},
		{ID: 11, Name: "Verify Postgres Backend", Status: "completed", Conclusion: "success", HTMLURL: "https://github.test/postgres"},
	}, []string{"Offline CI"})

	if result.Status != GitHubCheckGateStatusPending || !result.AllObservedChecksTerminal {
		t.Fatalf("result = %+v", result)
	}
	if len(result.MissingRequiredChecks) != 1 || result.MissingRequiredChecks[0] != "Offline CI" {
		t.Fatalf("missing checks = %#v", result.MissingRequiredChecks)
	}
	if got := githubCheckRunNames(result.ObservedCheckRuns); len(got) != 2 || got[0] != "Verify Offline" || got[1] != "Verify Postgres Backend" {
		t.Fatalf("observed names = %#v", got)
	}
	if !strings.Contains(result.Summary, "Verify Offline") {
		t.Fatalf("summary = %q", result.Summary)
	}
}

func TestEvaluateGitHubCheckRunsWaitsForLateObservedRun(t *testing.T) {
	result := evaluateGitHubCheckRuns([]githubCheckRunResponse{
		{ID: 10, Name: "setup", Status: "in_progress"},
	}, []string{"Verify Offline"})

	if result.Status != GitHubCheckGateStatusPending || result.AllObservedChecksTerminal {
		t.Fatalf("result = %+v", result)
	}
}

func TestEvaluateGitHubCheckRunsReportsPartialRequiredMatch(t *testing.T) {
	result := evaluateGitHubCheckRuns([]githubCheckRunResponse{
		{ID: 10, Name: "Verify Offline", Status: "completed", Conclusion: "success"},
		{ID: 11, Name: "Verify Postgres Backend", Status: "completed", Conclusion: "success"},
	}, []string{"Verify Offline", "CI"})

	if result.Status != GitHubCheckGateStatusPending || len(result.CheckRuns) != 2 || len(result.MissingRequiredChecks) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.MissingRequiredChecks[0] != "CI" || len(result.ObservedCheckRuns) != 2 {
		t.Fatalf("partial diagnostics = %+v", result)
	}
}

func TestEvaluateGitHubCheckRunsKeepsLatestRerunByName(t *testing.T) {
	result := evaluateGitHubCheckRuns([]githubCheckRunResponse{
		{ID: 10, Name: "Verify Offline", Status: "completed", Conclusion: "failure"},
		{ID: 20, Name: "Verify Offline", Status: "completed", Conclusion: "success"},
	}, []string{"Verify Offline"})

	if result.Status != GitHubCheckGateStatusPassed || len(result.CheckRuns) != 1 || result.CheckRuns[0].Conclusion != "success" {
		t.Fatalf("result = %+v", result)
	}
}

func TestGitHubClientDiscoversLatestObservedRunsWithoutRequiredNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"check_runs":[
			{"id":10,"name":"Verify","status":"completed","conclusion":"failure","html_url":"https://github.test/old"},
			{"id":20,"name":"Verify","status":"completed","conclusion":"success","html_url":"https://github.test/new"},
			{"id":30,"name":"Lint","status":"in_progress","details_url":"https://github.test/lint"}
		]}`))
	}))
	defer server.Close()
	client := NewGitHubClient(server.URL, "", time.Second)

	result, err := client.CheckCommit(context.Background(), "owner/repo", "0123456789abcdef0123456789abcdef01234567", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ObservedCheckRuns) != 2 ||
		result.ObservedCheckRuns[0].Name != "Lint" ||
		result.ObservedCheckRuns[1].URL != "https://github.test/new" ||
		result.AllObservedChecksTerminal {
		t.Fatalf("observed runs = %+v", result)
	}
}

func TestEvaluateGitHubCheckRunsPreservesQueueAndRunTimestamps(t *testing.T) {
	createdAt := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	startedAt := createdAt.Add(20 * time.Second)
	completedAt := startedAt.Add(40 * time.Second)
	result := evaluateGitHubCheckRuns([]githubCheckRunResponse{{
		ID: 10, Name: "Verify", Status: "completed", Conclusion: "success",
		CreatedAt: &createdAt, StartedAt: &startedAt, CompletedAt: &completedAt,
	}}, []string{"Verify"})
	if len(result.CheckRuns) != 1 || result.CheckRuns[0].CreatedAt == nil || result.CheckRuns[0].StartedAt == nil || result.CheckRuns[0].CompletedAt == nil {
		t.Fatalf("timestamps missing: %+v", result.CheckRuns)
	}
	if got := githubCheckDetectionLag(completedAt.Add(5*time.Second), result.CheckRuns); got != 5*time.Second {
		t.Fatalf("detection lag = %s", got)
	}
	queueTime, runTime := githubCheckQueueAndRunTime(result.CheckRuns)
	if queueTime != 20*time.Second || runTime != 40*time.Second {
		t.Fatalf("queue=%s run=%s", queueTime, runTime)
	}
}
