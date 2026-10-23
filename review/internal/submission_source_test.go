package review

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestManagedReviewSourceCheckedBeforeRoundOrGate(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		code   string
	}{
		{"ancestor", 200, `{"status":"ahead"}`, ""},
		{"same commit", 200, `{"status":"identical"}`, ""},
		{"unknown head", 404, `{"message":"Not Found"}`, "unknown_commit"},
		{"unknown base", 422, `{"message":"No commit found for SHA"}`, "unknown_commit"},
		{"base after head", 200, `{"status":"behind"}`, "base_not_ancestor"},
		{"unrelated branch", 200, `{"status":"diverged"}`, "base_not_ancestor"},
		{"unrelated roots", 404, `{"message":"No common ancestor between the two commits"}`, "base_not_ancestor"},
		{"GitHub unavailable", 503, `{"message":"unavailable"}`, "source_validation_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			source := &ReviewSourceRequest{Repository: "owner/repo", CommitSHA: strings.Repeat("a", 40), BaseCommit: strings.Repeat("b", 40)}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				want := "/repos/owner/repo/compare/" + source.BaseCommit + "..." + source.CommitSHA
				if r.URL.Path != want {
					t.Errorf("request path = %s, want %s", r.URL.Path, want)
				}
				w.WriteHeader(test.status)
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			store := newMemoryStore()
			messages := &fakeMessages{}
			tasks := &fakeTasks{tasks: map[int64]TaskContext{42: {ID: 42, ProjectID: "den-services", Status: TaskStatusInProgress}}}
			service := newTestService(store, messages, tasks)
			service.ConfigureGitHubChecks(NewGitHubClient(server.URL, "", time.Second), DefaultGitHubCheckOptions())
			_, err := service.RequestReview(context.Background(), "den-services", 42, CreateReviewRoundRequest{RequestedBy: "coder", Notes: "Ready", Source: source})
			if test.code == "" {
				if err != nil || len(store.rounds) != 1 {
					t.Fatalf("valid source: rounds=%d err=%v", len(store.rounds), err)
				}
			} else {
				var serviceErr *ServiceError
				if !errors.As(err, &serviceErr) || serviceErr.Code() != test.code {
					t.Fatalf("err=%v, want %s", err, test.code)
				}
				if len(store.rounds) != 0 || len(store.githubCheckGates) != 0 || len(messages.appended) != 0 || len(tasks.statusUpdates) != 0 {
					t.Fatal("rejected source mutated review, gate, message, or task state")
				}
			}
			if calls != 1 {
				t.Fatalf("GitHub calls=%d, want 1", calls)
			}
		})
	}
}

func TestReviewSupersessionClosesOldRoundAndRecordsReasonOnce(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	messages := &fakeMessages{}
	tasks := &fakeTasks{tasks: map[int64]TaskContext{42: {ID: 42, ProjectID: "den-services", Status: TaskStatusInProgress}}}
	service := newTestService(store, messages, tasks)
	first, err := service.RequestReview(ctx, "den-services", 42, CreateReviewRoundRequest{RequestedBy: "coder", Notes: "Old range"})
	if err != nil {
		t.Fatal(err)
	}
	request := CreateReviewRoundRequest{RequestedBy: "coder", Notes: "Correct range", SupersedesRound: first.ReviewRoundID, SupersedeReason: "Corrected the diff base."}
	replacement, err := service.RequestReview(ctx, "den-services", 42, request)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := store.GetRound(ctx, *first.ReviewRoundID)
	if old.Verdict != VerdictSuperseded || !strings.Contains(old.VerdictNotes, "Corrected the diff base.") || !strings.Contains(old.VerdictNotes, fmt.Sprint(*replacement.ReviewRoundID)) {
		t.Fatalf("old round=%+v", old)
	}
	if len(messages.appended) != 2 || !strings.Contains(messages.appended[1].Content, "Corrected the diff base.") {
		t.Fatalf("messages=%+v", messages.appended)
	}
	replayed, err := service.RequestReview(ctx, "den-services", 42, request)
	if err != nil || *replayed.ReviewRoundID != *replacement.ReviewRoundID || len(store.rounds) != 2 || len(messages.appended) != 2 {
		t.Fatalf("replay=%+v err=%v rounds=%d messages=%d", replayed, err, len(store.rounds), len(messages.appended))
	}
	_, err = service.CreateFinding(ctx, old.ID, CreateReviewFindingRequest{CreatedBy: "reviewer", Category: CategoryFollowUpCandidate, Summary: "Late finding"})
	if !errors.Is(err, ErrStaleReviewRound) {
		t.Fatalf("late finding err=%v", err)
	}
}

func TestReviewSupersessionPreservesExistingReviewEvidence(t *testing.T) {
	for _, kind := range []string{"verdict", "finding", "other task", "missing"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			store := newMemoryStore()
			tasks := &fakeTasks{tasks: map[int64]TaskContext{42: {ID: 42, ProjectID: "den-services", Status: TaskStatusInProgress}}}
			service := newTestService(store, &fakeMessages{}, tasks)
			old, err := store.CreateRound(ctx, &ReviewRound{ProjectID: "den-services", TaskID: 42, RequestedBy: "coder"})
			if err != nil {
				t.Fatal(err)
			}
			id := old.ID
			switch kind {
			case "verdict":
				store.rounds[id].Verdict = VerdictLooksGood
			case "finding":
				store.findings[1] = &ReviewFinding{ReviewRoundID: id}
			case "other task":
				store.rounds[id].TaskID = 43
			case "missing":
				id = 99
			}
			_, err = service.RequestReview(ctx, "den-services", 42, CreateReviewRoundRequest{RequestedBy: "coder", SupersedesRound: &id})
			var serviceErr *ServiceError
			if !errors.As(err, &serviceErr) || serviceErr.Code() != "round_not_supersedable" || len(store.rounds) != 1 {
				t.Fatalf("err=%v rounds=%d", err, len(store.rounds))
			}
		})
	}
}
