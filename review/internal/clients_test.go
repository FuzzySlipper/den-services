package review

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPTaskClientSetTaskStatusUsesProjectTaskPatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/v1/projects/den-services/tasks/42" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer task-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var body struct {
			Agent  string `json:"agent"`
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if body.Agent != "codex" || body.Status != TaskStatusReview {
			t.Fatalf("body = %+v", body)
		}
		_, _ = w.Write([]byte(`{"id":42,"project_id":"den-services","title":"Gate task","status":"review","priority":2}`))
	}))
	defer server.Close()

	client := NewTaskClient(server.URL, "task-token")
	task, err := client.SetTaskStatus(t.Context(), "den-services", 42, "codex", TaskStatusReview)
	if err != nil {
		t.Fatalf("SetTaskStatus() error = %v", err)
	}
	if task.ID != 42 || task.Status != TaskStatusReview {
		t.Fatalf("task = %+v", task)
	}
}

func TestHTTPTaskClientTransitionTaskToReviewUsesConditionalEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/projects/den-services/tasks/42/transitions/review" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Agent string `json:"agent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Agent != "codex" {
			t.Fatalf("agent = %q", body.Agent)
		}
		_, _ = w.Write([]byte(`{
			"task":{"id":42,"project_id":"den-services","title":"Gate task","status":"review","priority":2},
			"task_transition":"transitioned",
			"resulting_task_status":"review"
		}`))
	}))
	defer server.Close()

	result, err := NewTaskClient(server.URL, "").TransitionTaskToReview(t.Context(), "den-services", 42, "codex")
	if err != nil {
		t.Fatalf("TransitionTaskToReview() error = %v", err)
	}
	if result.Task.Status != TaskStatusReview || result.Transition != TaskTransitionApplied {
		t.Fatalf("result = %+v", result)
	}
}

func TestHTTPTaskClientTransitionTaskToReviewPreservesIneligibleConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"code":"review_transition_ineligible","message":"current status is done"}}`, http.StatusConflict)
	}))
	defer server.Close()

	_, err := NewTaskClient(server.URL, "").TransitionTaskToReview(t.Context(), "den-services", 42, "codex")
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Code() != "review_transition_ineligible" ||
		serviceErr.HTTPStatus() != http.StatusConflict {
		t.Fatalf("TransitionTaskToReview() error = %#v", err)
	}
}

func TestHTTPTaskClientDecodesCampaignMembershipFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/child-project/tasks/7001" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"task":{"id":7001,"project_id":"child-project","parent_id":6212,"title":"Child","status":"done","priority":2,"tags":["campaign:den-services:6212"]}}`))
	}))
	defer server.Close()

	task, err := NewTaskClient(server.URL, "").GetTaskContext(t.Context(), "child-project", 7001)
	if err != nil {
		t.Fatalf("GetTaskContext() error = %v", err)
	}
	if task.ParentID == nil || *task.ParentID != 6212 || len(task.Tags) != 1 || task.Tags[0] != "campaign:den-services:6212" {
		t.Fatalf("task = %+v", task)
	}
}
