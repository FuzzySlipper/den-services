package backend

import (
	"context"
	"io"
	"net/http"
	"testing"

	"den-services/mcp/internal/config"
)

func TestBuildBoardRelayRESTRequestRoutesSyncWithServiceAuth(t *testing.T) {
	request, err := buildBoardRelayRESTRequest(context.Background(), config.BackendConfig{BaseURL: "http://example.test", ServiceToken: "relay-token"}, Route{
		Operation: "sync_board_github", Method: http.MethodPost, Path: "/v1/projects/{project_id}/board/github-sync",
	}, ToolCall{Arguments: []byte(`{"project_id":"rusty dagger"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if got := request.URL.String(); got != "http://example.test/v1/projects/rusty%20dagger/board/github-sync" {
		t.Fatalf("url = %q", got)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer relay-token" {
		t.Fatalf("authorization = %q", got)
	}
}

func TestBuildBoardRelayRESTRequestEncodesVisibility(t *testing.T) {
	request, err := buildBoardRelayRESTRequest(context.Background(), config.BackendConfig{BaseURL: "http://example.test"}, Route{
		Operation: "set_board_github_visibility", Method: http.MethodPatch, Path: "/v1/board/github-visibility",
	}, ToolCall{Arguments: []byte(`{"visibility":"private"}`)})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); got != `{"visibility":"private"}` {
		t.Fatalf("body = %s", got)
	}
}
