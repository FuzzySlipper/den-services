package review

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"den-services/shared/health"
)

func TestReviewServerProtectsAPIByDefault(t *testing.T) {
	server := newReviewServerForAuthTest(t, false)

	request := httptest.NewRequest(http.MethodGet, "/v1/projects/den-services/tasks/42/review/rounds", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestReviewServerExposesTerminalGateEventCursor(t *testing.T) {
	store := newMemoryStore()
	completedAt := time.Now().UTC()
	store.recordGitHubCheckGateEvent(&GitHubCheckGate{
		ID: 7, ProjectID: "den-services", TaskID: 42, Repository: "owner/repo", CommitSHA: "abc", Ref: "main",
		Status: GitHubCheckGateStatusSuperseded, TerminalReason: GitHubCheckTerminalReasonSuperseded,
		RequestedBy: "codex", CreatedAt: completedAt.Add(-time.Minute), CompletedAt: &completedAt,
	}, completedAt)
	service := newTestService(store, &fakeMessages{}, &fakeTasks{})
	info, _ := health.NewBuildInfo("review", "0.1.0", "testcommit", completedAt)
	server, err := NewHTTPServer(&Config{
		BindAddr: "127.0.0.1:0", ServiceToken: "token", AllowUnauthenticatedLocalDev: true,
		HTTP: HTTPConfig{ReadHeaderTimeout: 5 * time.Second},
	}, info, service)
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/projects/den-services/review/github-check-gate-events?after_id=0&task_id=42", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	var page GitHubCheckGateEventPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode response: %v body=%s", err, response.Body.String())
	}
	if response.Code != http.StatusOK || len(page.Events) != 1 || page.Events[0].GateID != 7 || page.NextCursor != 1 {
		t.Fatalf("response code=%d page=%+v", response.Code, page)
	}
}

func TestReviewServerAllowsExplicitUnauthenticatedLocalDev(t *testing.T) {
	server := newReviewServerForAuthTest(t, true)

	request := httptest.NewRequest(http.MethodGet, "/v1/projects/den-services/tasks/42/review/rounds", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", response.Code, http.StatusOK, response.Body.String())
	}
}

func TestReviewServerFinalizesReview(t *testing.T) {
	store := newMemoryStore()
	store.rounds[7] = &ReviewRound{
		ID: 7, ProjectID: "den-services", TaskID: 42, RoundNumber: 1,
		RequestedBy: "implementer", HeadCommit: "abc", Verdict: "",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	service := newTestService(store, &fakeMessages{}, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Finalize route", Status: TaskStatusReview, Priority: 1},
	}})
	info, _ := health.NewBuildInfo("review", "0.1.0", "testcommit", time.Now().UTC())
	server, err := NewHTTPServer(&Config{
		BindAddr: "127.0.0.1:0", ServiceToken: "token", AllowUnauthenticatedLocalDev: true,
		HTTP: HTTPConfig{ReadHeaderTimeout: 5 * time.Second},
	}, info, service)
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	body := bytes.NewBufferString(`{"review_round_id":7,"verdict":"looks_good","decided_by":"reviewer"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/review/finalizations", body)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	var receipt ReviewFinalizationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil {
		t.Fatalf("decode response: %v body=%s", err, response.Body.String())
	}
	if response.Code != http.StatusOK || receipt.State != FinalizationStateComplete || receipt.ResultingTaskStatus != TaskStatusDone {
		t.Fatalf("response code=%d receipt=%+v body=%s", response.Code, receipt, response.Body.String())
	}
}

func TestReviewServerDiscoversGitHubChecksWithoutTaskContext(t *testing.T) {
	service := newTestService(newMemoryStore(), &fakeMessages{}, &fakeTasks{})
	service.ConfigureGitHubChecks(&fakeGitHubChecks{result: GitHubCheckResult{
		ObservedCheckRuns:         []GitHubCheckRun{{Name: "Verify", Status: "completed", Conclusion: "success"}},
		AllObservedChecksTerminal: true,
	}}, DefaultGitHubCheckOptions())
	info, _ := health.NewBuildInfo("review", "0.1.0", "testcommit", time.Now().UTC())
	server, err := NewHTTPServer(&Config{
		BindAddr: "127.0.0.1:0", ServiceToken: "token", AllowUnauthenticatedLocalDev: true,
		HTTP: HTTPConfig{ReadHeaderTimeout: 5 * time.Second},
	}, info, service)
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"repository":"owner/repo","commit_sha":"0123456789abcdef0123456789abcdef01234567","required_checks":["Verify"]}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/review/github-checks/discover", body)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)

	var discovery GitHubCheckDiscoveryResponse
	if err := json.Unmarshal(response.Body.Bytes(), &discovery); err != nil {
		t.Fatalf("decode response: %v body=%s", err, response.Body.String())
	}
	if response.Code != http.StatusOK || discovery.ConfigurationStatus != GitHubCheckDiscoveryValid ||
		len(discovery.ObservedCheckRuns) != 1 {
		t.Fatalf("response code=%d discovery=%+v body=%s", response.Code, discovery, response.Body.String())
	}
}

func newReviewServerForAuthTest(t *testing.T, allowUnauthenticated bool) *http.Server {
	t.Helper()

	service := newTestService(newMemoryStore(), &fakeMessages{}, &fakeTasks{tasks: map[int64]TaskContext{
		42: {ID: 42, ProjectID: "den-services", Title: "Review auth", Status: TaskStatusReview, Priority: 1},
	}})
	info, err := health.NewBuildInfo("review", "0.1.0", "testcommit", time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	server, err := NewHTTPServer(&Config{
		BindAddr:                     "127.0.0.1:0",
		ServiceToken:                 "review-token",
		AllowUnauthenticatedLocalDev: allowUnauthenticated,
		HTTP:                         HTTPConfig{ReadHeaderTimeout: 5 * time.Second},
	}, info, service)
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	return server
}
