package board

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandlerCreatesAndIncrementallyListsBoardComments(t *testing.T) {
	service := NewService(NewMemoryStore(), NoopProjectValidator{}, fixedClock())
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)

	postResponse := performJSON(t, mux, http.MethodPost, "/v1/projects/project-a/board/posts", `{"title":"Topic","body_markdown":"Body","author_identity":"human"}`, http.StatusCreated)
	var post PostResponse
	decodeTestJSON(t, postResponse, &post)
	rootResponse := performJSON(t, mux, http.MethodPost, "/v1/board/posts/1/comments", `{"body_markdown":"Root","author_identity":"agent"}`, http.StatusCreated)
	var root CommentResponse
	decodeTestJSON(t, rootResponse, &root)
	performJSON(t, mux, http.MethodPost, "/v1/board/posts/1/comments", `{"parent_comment_id":2,"body_markdown":"Child","author_identity":"human"}`, http.StatusCreated)

	pageResponse := performJSON(t, mux, http.MethodGet, "/v1/board/posts/1/comments?parent_comment_id=2", "", http.StatusOK)
	var page CommentPageResponse
	decodeTestJSON(t, pageResponse, &page)
	if post.ID != 1 || root.ID != 2 || len(page.Comments) != 1 || page.Comments[0].ParentCommentID == nil || *page.Comments[0].ParentCommentID != root.ID {
		t.Fatalf("post=%#v root=%#v page=%#v", post, root, page)
	}
}

func TestHandlerPurgeReturnsNoContentAndThenNonRevealingNotFound(t *testing.T) {
	service := NewService(NewMemoryStore(), NoopProjectValidator{}, func() time.Time { return time.Now().UTC() })
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)
	performJSON(t, mux, http.MethodPost, "/v1/projects/project-a/board/posts", `{"title":"Topic","body_markdown":"Body","author_identity":"human"}`, http.StatusCreated)
	performJSON(t, mux, http.MethodDelete, "/v1/board/posts/1", `{"actor_identity":"moderator","reason":"misleading"}`, http.StatusNoContent)
	response := performJSON(t, mux, http.MethodGet, "/v1/board/posts/1", "", http.StatusNotFound)
	if bytes.Contains(response.Body.Bytes(), []byte("Topic")) || bytes.Contains(response.Body.Bytes(), []byte("human")) {
		t.Fatalf("purged response leaked authored content: %s", response.Body.String())
	}
}

func performJSON(t *testing.T, handler http.Handler, method, path, body string, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d: %s", method, path, response.Code, wantStatus, response.Body.String())
	}
	return response
}

func decodeTestJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatal(err)
	}
}
