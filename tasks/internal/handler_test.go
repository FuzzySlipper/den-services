package tasks

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"den-services/shared/health"
)

func TestHTTPTasksLifecycle(t *testing.T) {
	server := testServer(t)

	createDependency := authedJSONRequest(http.MethodPost, "/v1/projects/den-services/tasks", `{
		"title": "Dependency",
		"priority": 2
	}`)
	dependencyResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(dependencyResponse, createDependency)
	if dependencyResponse.Code != http.StatusCreated {
		t.Fatalf("create dependency status = %d body = %s", dependencyResponse.Code, dependencyResponse.Body.String())
	}
	var dependency TaskResponse
	decodeJSON(t, dependencyResponse.Body, &dependency)

	createTask := authedJSONRequest(http.MethodPost, "/v1/projects/den-services/tasks", `{
		"title": "Main task",
		"priority": 1,
		"tags": ["infra", "tasks"],
		"depends_on": [`+int64String(&dependency.ID)+`]
	}`)
	taskResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(taskResponse, createTask)
	if taskResponse.Code != http.StatusCreated {
		t.Fatalf("create task status = %d body = %s", taskResponse.Code, taskResponse.Body.String())
	}
	var task TaskResponse
	decodeJSON(t, taskResponse.Body, &task)

	listResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(listResponse, authedJSONRequest(http.MethodGet, "/v1/projects/den-services/tasks?tags=infra,tasks", ""))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", listResponse.Code, listResponse.Body.String())
	}
	var listed []TaskSummaryResponse
	decodeJSON(t, listResponse.Body, &listed)
	if len(listed) != 1 || listed[0].ID != task.ID || listed[0].Availability != AvailabilityWaitingOnDependencies {
		t.Fatalf("listed = %+v", listed)
	}

	nextResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(nextResponse, authedJSONRequest(http.MethodGet, "/v1/projects/den-services/tasks/next", ""))
	if nextResponse.Code != http.StatusOK {
		t.Fatalf("next status = %d body = %s", nextResponse.Code, nextResponse.Body.String())
	}
	var next TaskResponse
	decodeJSON(t, nextResponse.Body, &next)
	if next.ID != dependency.ID {
		t.Fatalf("next = %+v, want dependency %+v", next, dependency)
	}

	patchDone := authedJSONRequest(http.MethodPatch, "/v1/tasks/"+int64String(&dependency.ID), `{
		"agent": "tester",
		"status": "done"
	}`)
	patchResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(patchResponse, patchDone)
	if patchResponse.Code != http.StatusOK {
		t.Fatalf("patch status = %d body = %s", patchResponse.Code, patchResponse.Body.String())
	}

	detailResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(detailResponse, authedJSONRequest(http.MethodGet, "/v1/projects/den-services/tasks/"+int64String(&task.ID), ""))
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail status = %d body = %s", detailResponse.Code, detailResponse.Body.String())
	}
	var detail TaskDetailResponse
	decodeJSON(t, detailResponse.Body, &detail)
	if len(detail.Dependencies) != 1 || detail.Dependencies[0].TaskID != dependency.ID {
		t.Fatalf("detail dependencies = %+v", detail.Dependencies)
	}
}

func TestHTTPRequiresAuth(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/projects/den-services/tasks", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}

func testServer(t *testing.T) *http.Server {
	t.Helper()
	cfg := &Config{
		BindAddr:     "127.0.0.1:0",
		DatabaseURL:  "postgres://unused",
		ServiceToken: "test-token",
		HTTP:         HTTPConfig{ReadHeaderTimeout: time.Second},
	}
	server, err := NewHTTPServer(cfg, testBuildInfo(t), newTestService())
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	return server
}

func authedJSONRequest(method string, path string, body string) *http.Request {
	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Authorization", "Bearer test-token")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func decodeJSON(t *testing.T, reader io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(reader).Decode(target); err != nil {
		t.Fatalf("decoding json: %v", err)
	}
}

func testBuildInfo(t *testing.T) health.BuildInfo {
	t.Helper()
	info, err := health.NewBuildInfo("tasks", "test", "abcdef", fixedClock())
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	return info
}
