package backend

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"den-services/mcp/internal/config"
)

func TestBuildBoardRESTRequestWalksOnlyDirectChildren(t *testing.T) {
	parentID := int64(23)
	afterID := int64(29)
	limit := 7
	request, err := buildBoardRESTRequest(context.Background(), config.BackendConfig{BaseURL: "http://example.test", ServiceToken: "secret"}, Route{
		Operation: "list_board_comments", Method: http.MethodGet, Path: "/v1/board/posts/{post_id}/comments",
	}, ToolCall{Arguments: mustBoardArguments(t, boardToolArguments{PostID: 11, ParentCommentID: &parentID, AfterID: &afterID, Limit: &limit})})
	if err != nil {
		t.Fatal(err)
	}
	if got := request.URL.String(); got != "http://example.test/v1/board/posts/11/comments?after_id=29&limit=7&parent_comment_id=23" {
		t.Fatalf("url = %q", got)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("authorization = %q", got)
	}
}

func TestBuildBoardRESTRequestCreatesImmediateReply(t *testing.T) {
	parentID := int64(23)
	request, err := buildBoardRESTRequest(context.Background(), config.BackendConfig{BaseURL: "http://example.test"}, Route{
		Operation: "create_board_comment", Method: http.MethodPost, Path: "/v1/board/posts/{post_id}/comments",
	}, ToolCall{Arguments: mustBoardArguments(t, boardToolArguments{PostID: 11, ParentCommentID: &parentID, BodyMarkdown: "reply", AuthorIdentity: "agent"})})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(body), `{"parent_comment_id":23,"body_markdown":"reply","author_identity":"agent"}`; got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestBuildBoardRESTRequestSearchesWithBoundedQuery(t *testing.T) {
	afterID := int64(29)
	limit := 7
	request, err := buildBoardRESTRequest(context.Background(), config.BackendConfig{BaseURL: "http://example.test", ServiceToken: "secret"}, Route{
		Operation: "search_board_posts", Method: http.MethodGet, Path: "/v1/projects/{project_id}/board/posts/search",
	}, ToolCall{Arguments: mustBoardArguments(t, boardToolArguments{ProjectID: "den services", Query: "thread tree", AfterID: &afterID, Limit: &limit})})
	if err != nil {
		t.Fatal(err)
	}
	if got := request.URL.String(); got != "http://example.test/v1/projects/den%20services/board/posts/search?after_id=29&limit=7&q=thread+tree" {
		t.Fatalf("url = %q", got)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("authorization = %q", got)
	}
}

func TestBoardRESTPurgesWithModerationMetadata(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"purged":true}`))
	}))
	defer server.Close()
	client := NewClient(server.Client())
	result, failure, err := client.Call(context.Background(), config.BackendConfig{Name: "board", BaseURL: server.URL, Timeout: time.Second}, Route{
		Operation: "purge_board_comment", Backend: "board", Method: http.MethodDelete, Path: "/v1/board/comments/{comment_id}", RequestAdapter: RequestAdapterMCPBoardREST, ResponseAdapter: ResponseAdapterMCPToolResultJSON,
	}, ToolCall{ToolName: "purge_board_comment", Operation: "purge_board_comment", Arguments: mustBoardArguments(t, boardToolArguments{CommentID: 41, ActorIdentity: "moderator", Reason: "misleading"})})
	if err != nil || failure != nil {
		t.Fatalf("err = %v, failure = %#v", err, failure)
	}
	if gotMethod != http.MethodDelete || gotPath != "/v1/board/comments/41" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotBody != `{"actor_identity":"moderator","reason":"misleading"}` {
		t.Fatalf("body = %s", gotBody)
	}
	if len(result.Value) == 0 {
		t.Fatal("missing MCP result")
	}
}

func mustBoardArguments(t *testing.T, arguments boardToolArguments) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
