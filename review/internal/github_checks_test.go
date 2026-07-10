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
