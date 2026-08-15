package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBoardSearchBuildsPathQueryAndBearerAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", request.Method)
		}
		if request.URL.Path != "/v1/projects/den-services/board/posts/search" {
			t.Errorf("path = %s, want board search path", request.URL.Path)
		}
		if got := request.URL.Query().Get("q"); got != "needle words" {
			t.Errorf("q = %q, want needle words", got)
		}
		if got := request.URL.Query().Get("after_id"); got != "7" {
			t.Errorf("after_id = %q, want 7", got)
		}
		if got := request.URL.Query().Get("limit"); got != "20" {
			t.Errorf("limit = %q, want 20", got)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("authorization = %q, want bearer token", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"posts":[{"id":1}]}`))
	}))
	defer server.Close()

	client, err := NewBoardClient(server.URL, "test-token", server.Client())
	if err != nil {
		t.Fatalf("NewBoardClient() error = %v", err)
	}
	afterID := int64(7)
	limit := 20
	body, err := client.Search(context.Background(), BoardSearchOptions{
		ProjectID: "den-services",
		Query:     "needle words",
		AfterID:   &afterID,
		Limit:     &limit,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if string(body) != `{"posts":[{"id":1}]}` {
		t.Fatalf("body = %q, want service JSON unchanged", body)
	}
}

func TestBoardSearchOmitsOptionalQueryParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, ok := request.URL.Query()["after_id"]; ok {
			t.Errorf("after_id unexpectedly present: %q", request.URL.RawQuery)
		}
		if _, ok := request.URL.Query()["limit"]; ok {
			t.Errorf("limit unexpectedly present: %q", request.URL.RawQuery)
		}
		_, _ = io.WriteString(writer, "[]")
	}))
	defer server.Close()

	client, err := NewBoardClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatalf("NewBoardClient() error = %v", err)
	}
	if _, err := client.Search(context.Background(), BoardSearchOptions{ProjectID: "den-web", Query: "query"}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestBoardClientCoversPostCommentTraversalAndPurgeRoutes(t *testing.T) {
	type observedRequest struct {
		Method string
		Path   string
		Query  string
		Body   map[string]any
	}
	requests := make([]observedRequest, 0, 9)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed := observedRequest{Method: request.Method, Path: request.URL.Path, Query: request.URL.RawQuery}
		if request.Body != nil && request.ContentLength != 0 {
			if err := json.NewDecoder(request.Body).Decode(&observed.Body); err != nil {
				t.Errorf("decode request body: %v", err)
			}
		}
		requests = append(requests, observed)
		if request.Method == http.MethodDelete {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"ok":true}`)
	}))
	defer server.Close()

	client, err := NewBoardClient(server.URL, "test-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	parentID := int64(7)
	afterID := int64(11)
	limit := 20
	calls := []func() ([]byte, error){
		func() ([]byte, error) {
			return client.CreatePost(ctx, BoardCreatePostOptions{ProjectID: "den-services", Title: "Topic", BodyMarkdown: "Body", AuthorIdentity: "agent"})
		},
		func() ([]byte, error) {
			return client.ListPosts(ctx, BoardListPostsOptions{ProjectID: "den-services", AfterID: &afterID, Limit: &limit})
		},
		func() ([]byte, error) { return client.GetPost(ctx, 42) },
		func() ([]byte, error) {
			return client.CreateComment(ctx, BoardCreateCommentOptions{PostID: 42, ParentCommentID: &parentID, BodyMarkdown: "Reply", AuthorIdentity: "agent"})
		},
		func() ([]byte, error) {
			return client.ListComments(ctx, BoardListCommentsOptions{PostID: 42, ParentCommentID: &parentID, AfterID: &afterID, Limit: &limit})
		},
		func() ([]byte, error) { return client.GetComment(ctx, 8) },
		func() ([]byte, error) { return client.GetCommentPath(ctx, 8, &limit) },
		func() ([]byte, error) {
			return client.PurgePost(ctx, BoardPurgeOptions{ID: 42, ActorIdentity: "moderator", Reason: "misleading"})
		},
		func() ([]byte, error) {
			return client.PurgeComment(ctx, BoardPurgeOptions{ID: 8, ActorIdentity: "moderator", Reason: "misleading"})
		},
	}
	for index, call := range calls {
		body, err := call()
		if err != nil {
			t.Fatalf("call %d: %v", index, err)
		}
		if index >= 7 && string(body) != `{"purged":true}` {
			t.Fatalf("purge body = %q", body)
		}
	}

	want := []struct{ method, path string }{
		{http.MethodPost, "/v1/projects/den-services/board/posts"},
		{http.MethodGet, "/v1/projects/den-services/board/posts"},
		{http.MethodGet, "/v1/board/posts/42"},
		{http.MethodPost, "/v1/board/posts/42/comments"},
		{http.MethodGet, "/v1/board/posts/42/comments"},
		{http.MethodGet, "/v1/board/comments/8"},
		{http.MethodGet, "/v1/board/comments/8/path"},
		{http.MethodDelete, "/v1/board/posts/42"},
		{http.MethodDelete, "/v1/board/comments/8"},
	}
	if len(requests) != len(want) {
		t.Fatalf("request count = %d, want %d", len(requests), len(want))
	}
	for index := range want {
		if requests[index].Method != want[index].method || requests[index].Path != want[index].path {
			t.Errorf("request %d = %s %s, want %s %s", index, requests[index].Method, requests[index].Path, want[index].method, want[index].path)
		}
	}
	if requests[3].Body["parent_comment_id"] != float64(7) || requests[3].Body["body_markdown"] != "Reply" {
		t.Errorf("create comment body = %#v", requests[3].Body)
	}
	if got := requests[4].Query; got != "after_id=11&limit=20&parent_comment_id=7" {
		t.Errorf("list comments query = %q", got)
	}
	if requests[7].Body["reason"] != "misleading" || requests[8].Body["actor_identity"] != "moderator" {
		t.Errorf("purge bodies = %#v %#v", requests[7].Body, requests[8].Body)
	}
}
