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

const (
	changeTestOwnSHA   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	changeTestLaterSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	changeTestHeadSHA  = "cccccccccccccccccccccccccccccccccccccccc"
)

func TestEvaluateGitHubChangeRunsTreatsCancelledRunAsMissing(t *testing.T) {
	result := evaluateGitHubChangeRuns([]githubCheckRunResponse{
		{ID: 10, Name: "verify", Status: "completed", Conclusion: "cancelled", HeadSHA: changeTestOwnSHA},
	}, nil, []string{"verify"})

	if result.Status != GitHubCheckGateStatusPending {
		t.Fatalf("cancelled run should leave the gate pending: %+v", result)
	}
}

func TestEvaluateGitHubChangeRunsAcceptsCheckFromLaterCommit(t *testing.T) {
	result := evaluateGitHubChangeRuns([]githubCheckRunResponse{
		{ID: 10, Name: "verify", Status: "completed", Conclusion: "success", HeadSHA: changeTestOwnSHA},
	}, []commitCheckRuns{
		{SHA: changeTestLaterSHA, Runs: []githubCheckRunResponse{{ID: 20, Name: "pair", Status: "completed", Conclusion: "cancelled", HeadSHA: changeTestLaterSHA}}},
		{SHA: changeTestHeadSHA, Runs: []githubCheckRunResponse{{ID: 30, Name: "pair", Status: "completed", Conclusion: "success", HeadSHA: changeTestHeadSHA}}},
	}, []string{"verify", "pair"})

	if result.Status != GitHubCheckGateStatusPassed || !strings.Contains(result.Summary, "later commit") {
		t.Fatalf("result = %+v", result)
	}
	satisfiedBy := map[string]string{}
	for _, run := range result.CheckRuns {
		satisfiedBy[run.Name] = run.HeadSHA
	}
	if satisfiedBy["verify"] != changeTestOwnSHA || satisfiedBy["pair"] != changeTestHeadSHA {
		t.Fatalf("satisfying commits = %#v", satisfiedBy)
	}
}

func TestEvaluateGitHubChangeRunsKeepsLaterFailurePending(t *testing.T) {
	result := evaluateGitHubChangeRuns(nil, []commitCheckRuns{
		{SHA: changeTestHeadSHA, Runs: []githubCheckRunResponse{{ID: 30, Name: "pair", Status: "completed", Conclusion: "failure", HeadSHA: changeTestHeadSHA}}},
	}, []string{"pair"})

	if result.Status != GitHubCheckGateStatusPending || len(result.MissingRequiredChecks) != 0 {
		t.Fatalf("a failure on a later commit should not fail this commit's gate: %+v", result)
	}
}

func TestEvaluateGitHubChangeRunsOwnFailureWaitsForRunningLaterCommit(t *testing.T) {
	own := []githubCheckRunResponse{{ID: 10, Name: "verify", Status: "completed", Conclusion: "failure", HeadSHA: changeTestOwnSHA}}

	failed := evaluateGitHubChangeRuns(own, []commitCheckRuns{
		{SHA: changeTestHeadSHA, Runs: []githubCheckRunResponse{{ID: 30, Name: "pair", Status: "completed", Conclusion: "success", HeadSHA: changeTestHeadSHA}}},
	}, []string{"verify"})
	if failed.Status != GitHubCheckGateStatusFailed {
		t.Fatalf("own failure without a later verify run should fail: %+v", failed)
	}

	running := evaluateGitHubChangeRuns(own, []commitCheckRuns{
		{SHA: changeTestHeadSHA, Runs: []githubCheckRunResponse{{ID: 30, Name: "verify", Status: "in_progress", HeadSHA: changeTestHeadSHA}}},
	}, []string{"verify"})
	if running.Status != GitHubCheckGateStatusPending {
		t.Fatalf("own failure with a later verify still running should wait: %+v", running)
	}

	passed := evaluateGitHubChangeRuns(own, []commitCheckRuns{
		{SHA: changeTestHeadSHA, Runs: []githubCheckRunResponse{{ID: 30, Name: "verify", Status: "completed", Conclusion: "success", HeadSHA: changeTestHeadSHA}}},
	}, []string{"verify"})
	if passed.Status != GitHubCheckGateStatusPassed {
		t.Fatalf("a later containing commit that passes should satisfy the gate: %+v", passed)
	}
}

func TestGitHubClientCheckChangeReadsNearestLaterCommitsUntilSatisfied(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/repos/owner/repo/commits/" + changeTestOwnSHA + "/check-runs":
			_, _ = w.Write([]byte(`{"check_runs":[{"id":1,"name":"verify","status":"completed","conclusion":"success"}]}`))
		case "/repos/owner/repo/compare/" + changeTestOwnSHA + "...main":
			_, _ = w.Write([]byte(`{"status":"ahead","commits":[{"sha":"` + changeTestLaterSHA + `"},{"sha":"` + changeTestHeadSHA + `"}]}`))
		case "/repos/owner/repo/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":7,"head_sha":"` + changeTestLaterSHA + `","status":"completed","conclusion":"success"}]}`))
		case "/repos/owner/repo/commits/" + changeTestLaterSHA + "/check-runs":
			_, _ = w.Write([]byte(`{"check_runs":[{"id":2,"name":"pair","status":"completed","conclusion":"success"}]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	result, err := NewGitHubClient(server.URL, "", time.Second).CheckChange(context.Background(), GitHubChangeQuery{
		Repository: "owner/repo", CommitSHA: changeTestOwnSHA, Ref: "main",
		RequiredChecks: []string{"verify", "pair"}, LaterCommitLimit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != GitHubCheckGateStatusPassed {
		t.Fatalf("result = %+v", result)
	}
	if len(paths) != 4 {
		t.Fatalf("expected own, compare, branch runs, and nearest later commit only; got %v", paths)
	}
}

func TestGitHubClientCheckChangeSkipsLaterCommitsWhenOwnChecksSuffice(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"check_runs":[{"id":1,"name":"verify","status":"in_progress"}]}`))
	}))
	defer server.Close()

	result, err := NewGitHubClient(server.URL, "", time.Second).CheckChange(context.Background(), GitHubChangeQuery{
		Repository: "owner/repo", CommitSHA: changeTestOwnSHA, Ref: "main",
		RequiredChecks: []string{"verify"}, LaterCommitLimit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != GitHubCheckGateStatusPending || calls != 1 {
		t.Fatalf("status=%s calls=%d", result.Status, calls)
	}
}

func TestGitHubClientCheckChangeIgnoresUncomparableRef(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/compare/") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"check_runs":[{"id":1,"name":"verify","status":"completed","conclusion":"failure"}]}`))
	}))
	defer server.Close()

	result, err := NewGitHubClient(server.URL, "", time.Second).CheckChange(context.Background(), GitHubChangeQuery{
		Repository: "owner/repo", CommitSHA: changeTestOwnSHA, Ref: "gone",
		RequiredChecks: []string{"verify"}, LaterCommitLimit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != GitHubCheckGateStatusFailed {
		t.Fatalf("result = %+v", result)
	}
}

func TestNearestAndHeadCommitsKeepsRefHead(t *testing.T) {
	commits := []string{"1", "2", "3", "4", "5"}
	got := nearestAndHeadCommits(commits, 3)
	if strings.Join(got, ",") != "1,2,5" {
		t.Fatalf("got %v", got)
	}
	if got := nearestAndHeadCommits(commits[:2], 3); strings.Join(got, ",") != "1,2" {
		t.Fatalf("got %v", got)
	}
}

func TestGitHubClientCheckChangeKeepsMissingCheckPendingWhileWorkflowQueued(t *testing.T) {
	for _, test := range []struct {
		name         string
		runStatus    string
		wantTerminal bool
	}{
		{name: "queued in concurrency group", runStatus: "pending", wantTerminal: false},
		{name: "all runs finished", runStatus: "completed", wantTerminal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/repos/owner/repo/commits/"+changeTestOwnSHA+"/check-runs":
					_, _ = w.Write([]byte(`{"check_runs":[{"id":1,"name":"verify","status":"completed","conclusion":"success"}]}`))
				case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/compare/"):
					_, _ = w.Write([]byte(`{"status":"identical","commits":[]}`))
				case r.URL.Path == "/repos/owner/repo/actions/runs" && r.URL.Query().Get("branch") == "main":
					_, _ = w.Write([]byte(`{"workflow_runs":[{"id":9,"head_sha":"` + changeTestOwnSHA + `","status":"` + test.runStatus + `"}]}`))
				default:
					t.Errorf("unexpected request %s", r.URL.String())
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			result, err := NewGitHubClient(server.URL, "", time.Second).CheckChange(context.Background(), GitHubChangeQuery{
				Repository: "owner/repo", CommitSHA: changeTestOwnSHA, Ref: "main",
				RequiredChecks: []string{"verify", "pair"}, LaterCommitLimit: 5,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != GitHubCheckGateStatusPending || result.AllObservedChecksTerminal != test.wantTerminal {
				t.Fatalf("status=%s allObservedTerminal=%v, want terminal=%v", result.Status, result.AllObservedChecksTerminal, test.wantTerminal)
			}
		})
	}
}

func TestGitHubClientCheckChangeFindsPassBeyondPositionalWindow(t *testing.T) {
	later := make([]string, 0, 8)
	for i := 0; i < 8; i++ {
		later = append(later, strings.Repeat(string(rune('0'+i)), 40))
	}
	passing := later[5]
	var read []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/commits/"+changeTestOwnSHA+"/check-runs":
			_, _ = w.Write([]byte(`{"check_runs":[]}`))
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/compare/"):
			commits := make([]string, 0, len(later))
			for _, sha := range later {
				commits = append(commits, `{"sha":"`+sha+`"}`)
			}
			_, _ = w.Write([]byte(`{"status":"ahead","commits":[` + strings.Join(commits, ",") + `]}`))
		case r.URL.Path == "/repos/owner/repo/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[
				{"id":1,"head_sha":"` + later[1] + `","status":"completed","conclusion":"cancelled"},
				{"id":2,"head_sha":"` + passing + `","status":"completed","conclusion":"success"}
			]}`))
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			sha := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/commits/"), "/check-runs")
			read = append(read, sha)
			if sha == passing {
				_, _ = w.Write([]byte(`{"check_runs":[{"id":5,"name":"pair","status":"completed","conclusion":"success"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"check_runs":[]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	result, err := NewGitHubClient(server.URL, "", time.Second).CheckChange(context.Background(), GitHubChangeQuery{
		Repository: "owner/repo", CommitSHA: changeTestOwnSHA, Ref: "main",
		RequiredChecks: []string{"pair"}, LaterCommitLimit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != GitHubCheckGateStatusPassed || len(result.CheckRuns) != 1 || result.CheckRuns[0].HeadSHA != passing {
		t.Fatalf("result = %+v", result)
	}
	if len(read) != 1 || read[0] != passing {
		t.Fatalf("expected only the commit with a passing workflow run to be read, got %v", read)
	}
}
