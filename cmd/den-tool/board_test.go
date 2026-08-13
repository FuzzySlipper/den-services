package main

import (
	"context"
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
