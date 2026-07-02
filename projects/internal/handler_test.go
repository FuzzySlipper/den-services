package projects

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

func TestHTTPProjectsAndSpacesLifecycle(t *testing.T) {
	server := testServer(t)

	createProject := authedJSONRequest(http.MethodPost, "/v1/projects", `{
		"id": "den-services",
		"name": "Den Services",
		"root_path": "/home/dev/den-services",
		"description": "successor services"
	}`)
	createProjectResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(createProjectResponse, createProject)
	if createProjectResponse.Code != http.StatusCreated {
		t.Fatalf("create project status = %d body = %s", createProjectResponse.Code, createProjectResponse.Body.String())
	}
	var created ScopeResponse
	decodeJSON(t, createProjectResponse.Body, &created)
	if created.Kind != KindProject || created.Visibility != VisibilityNormal || !created.Writable {
		t.Fatalf("created project = %+v", created)
	}

	patchProject := authedJSONRequest(http.MethodPatch, "/v1/projects/den-services", `{
		"root_path": "",
		"owner": "patch",
		"settings_json": {"lane":"lifeboat"}
	}`)
	patchProjectResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(patchProjectResponse, patchProject)
	if patchProjectResponse.Code != http.StatusOK {
		t.Fatalf("patch project status = %d body = %s", patchProjectResponse.Code, patchProjectResponse.Body.String())
	}
	var patched ScopeResponse
	decodeJSON(t, patchProjectResponse.Body, &patched)
	if patched.RootPath != "" || patched.Owner != "patch" {
		t.Fatalf("patched project = %+v", patched)
	}
	if string(patched.SettingsJSON) != `{"lane":"lifeboat"}` {
		t.Fatalf("settings_json = %s", string(patched.SettingsJSON))
	}

	createSpace := authedJSONRequest(http.MethodPost, "/v1/spaces", `{
		"id": "assistant-space",
		"name": "Assistant Space",
		"kind": "assistant",
		"visibility": "hidden"
	}`)
	createSpaceResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(createSpaceResponse, createSpace)
	if createSpaceResponse.Code != http.StatusCreated {
		t.Fatalf("create space status = %d body = %s", createSpaceResponse.Code, createSpaceResponse.Body.String())
	}

	defaultSpaces := listScopes(t, server.Handler, "/v1/spaces")
	if len(defaultSpaces) != 1 || defaultSpaces[0].ID != "den-services" {
		t.Fatalf("default spaces = %+v", defaultSpaces)
	}
	hiddenSpaces := listScopes(t, server.Handler, "/v1/spaces?include_hidden=true")
	if len(hiddenSpaces) != 2 {
		t.Fatalf("hidden-inclusive spaces = %+v", hiddenSpaces)
	}
	assistantSpaces := listScopes(t, server.Handler, "/v1/spaces?kind=assistant&include_hidden=true")
	if len(assistantSpaces) != 1 || assistantSpaces[0].ID != "assistant-space" {
		t.Fatalf("assistant spaces = %+v", assistantSpaces)
	}

	archive := authedJSONRequest(http.MethodPost, "/v1/spaces/assistant-space/archive", `{}`)
	archiveResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(archiveResponse, archive)
	if archiveResponse.Code != http.StatusOK {
		t.Fatalf("archive status = %d body = %s", archiveResponse.Code, archiveResponse.Body.String())
	}
	archivedSpaces := listScopes(t, server.Handler, "/v1/spaces?include_archived=true")
	if len(archivedSpaces) != 2 {
		t.Fatalf("archived-inclusive spaces = %+v", archivedSpaces)
	}

	assertArchived := authedJSONRequest(http.MethodPost, "/v1/scopes/assistant-space/assert-writable", `{}`)
	assertArchivedResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(assertArchivedResponse, assertArchived)
	if assertArchivedResponse.Code != http.StatusConflict {
		t.Fatalf("assert archived status = %d body = %s", assertArchivedResponse.Code, assertArchivedResponse.Body.String())
	}

	restore := authedJSONRequest(http.MethodPatch, "/v1/spaces/assistant-space/visibility", `{"visibility":"normal"}`)
	restoreResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(restoreResponse, restore)
	if restoreResponse.Code != http.StatusOK {
		t.Fatalf("restore status = %d body = %s", restoreResponse.Code, restoreResponse.Body.String())
	}
	assertRestored := authedJSONRequest(http.MethodPost, "/v1/scopes/assistant-space/assert-writable", `{}`)
	assertRestoredResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(assertRestoredResponse, assertRestored)
	if assertRestoredResponse.Code != http.StatusOK {
		t.Fatalf("assert restored status = %d body = %s", assertRestoredResponse.Code, assertRestoredResponse.Body.String())
	}

	deleteSpace := authedJSONRequest(http.MethodPost, "/v1/admin/spaces/assistant-space/delete", `{}`)
	deleteSpaceResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(deleteSpaceResponse, deleteSpace)
	if deleteSpaceResponse.Code != http.StatusOK {
		t.Fatalf("delete space status = %d body = %s", deleteSpaceResponse.Code, deleteSpaceResponse.Body.String())
	}
	var deleted DeleteSpaceResponse
	decodeJSON(t, deleteSpaceResponse.Body, &deleted)
	if !deleted.Deleted || deleted.Space.ID != "assistant-space" || deleted.DependencyCountsComplete {
		t.Fatalf("delete response = %+v", deleted)
	}
	getDeleted := authedJSONRequest(http.MethodGet, "/v1/spaces/assistant-space", "")
	getDeletedResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(getDeletedResponse, getDeleted)
	if getDeletedResponse.Code != http.StatusNotFound {
		t.Fatalf("get deleted status = %d body = %s", getDeletedResponse.Code, getDeletedResponse.Body.String())
	}
}

func TestHTTPAdminDeleteSpaceProtectsCoreScopesWithoutForce(t *testing.T) {
	server := testServer(t)
	createGlobal := authedJSONRequest(http.MethodPost, "/v1/spaces", `{
		"id": "_global",
		"name": "Global",
		"kind": "system"
	}`)
	createGlobalResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(createGlobalResponse, createGlobal)
	if createGlobalResponse.Code != http.StatusCreated {
		t.Fatalf("create global status = %d body = %s", createGlobalResponse.Code, createGlobalResponse.Body.String())
	}

	deleteGlobal := authedJSONRequest(http.MethodPost, "/v1/admin/spaces/_global/delete", `{}`)
	deleteGlobalResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(deleteGlobalResponse, deleteGlobal)
	if deleteGlobalResponse.Code != http.StatusConflict {
		t.Fatalf("delete protected status = %d body = %s", deleteGlobalResponse.Code, deleteGlobalResponse.Body.String())
	}

	forceDelete := authedJSONRequest(http.MethodPost, "/v1/admin/spaces/_global/delete", `{"force":true}`)
	forceDeleteResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(forceDeleteResponse, forceDelete)
	if forceDeleteResponse.Code != http.StatusOK {
		t.Fatalf("force delete protected status = %d body = %s", forceDeleteResponse.Code, forceDeleteResponse.Body.String())
	}
}

func TestHTTPRejectsInvalidVisibility(t *testing.T) {
	server := testServer(t)
	request := authedJSONRequest(http.MethodPost, "/v1/spaces", `{
		"id": "bad-space",
		"name": "Bad Space",
		"visibility": "gone"
	}`)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}

func TestHTTPRequiresAuth(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
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
	server, err := NewHTTPServer(cfg, testBuildInfo(t), NewService(newMemoryStore(), fixedClock))
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	return server
}

func listScopes(t *testing.T, handler http.Handler, path string) []ScopeResponse {
	t.Helper()
	request := authedJSONRequest(http.MethodGet, path, "")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list %s status = %d body = %s", path, response.Code, response.Body.String())
	}
	var decoded []ScopeResponse
	decodeJSON(t, response.Body, &decoded)
	return decoded
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

func fixedClock() time.Time {
	return time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
}

func testBuildInfo(t *testing.T) health.BuildInfo {
	t.Helper()
	info, err := health.NewBuildInfo("projects", "test", "abcdef", fixedClock())
	if err != nil {
		t.Fatalf("NewBuildInfo() error = %v", err)
	}
	return info
}
