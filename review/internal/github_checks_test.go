package review

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGitHubClientReturnsHTTPErrorDetails(t *testing.T) {
	resetAt := time.Date(2026, 7, 6, 12, 30, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.Header().Set("x-ratelimit-remaining", "0")
		w.Header().Set("x-ratelimit-reset", "1783341000")
		w.Header().Set("retry-after", "120")
		w.Header().Set("x-github-request-id", "request-1")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer server.Close()
	client := NewGitHubClient(server.URL, "", time.Second)

	_, err := client.CheckCommit(context.Background(), "owner/repo", "0123456789abcdef0123456789abcdef01234567", []string{"Verify Offline"})
	if err == nil {
		t.Fatal("expected GitHub HTTP error")
	}
	var githubErr *GitHubHTTPError
	if !errors.As(err, &githubErr) {
		t.Fatalf("error was not GitHubHTTPError: %T %v", err, err)
	}
	if githubErr.StatusCode != http.StatusForbidden || githubErr.Message != "API rate limit exceeded" {
		t.Fatalf("unexpected GitHub error details: %+v", githubErr)
	}
	if !githubErr.RateLimitRemainingSet || githubErr.RateLimitRemaining != 0 {
		t.Fatalf("rate remaining header not parsed: %+v", githubErr)
	}
	if !githubErr.RateLimitResetSet || !githubErr.RateLimitReset.Equal(resetAt) {
		t.Fatalf("rate reset header not parsed: %+v", githubErr)
	}
	if !githubErr.RetryAfterSet || githubErr.RetryAfter != 2*time.Minute {
		t.Fatalf("retry-after header not parsed: %+v", githubErr)
	}
	if githubErr.RequestID != "request-1" {
		t.Fatalf("request id not parsed: %+v", githubErr)
	}
}
