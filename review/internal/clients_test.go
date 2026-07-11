package review

import (
	"encoding/json"
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
