package backend

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestClientClassifiesDNSFailureAsRetryableUnavailable(t *testing.T) {
	client := NewClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, &net.DNSError{
				Err:  "no such host",
				Name: "den-core.invalid",
			}
		}),
	})

	_, failure, err := client.Call(context.Background(), testBackend("den-core", "http://den-core.invalid"), testRoute("get_task", "den-core"), ToolCall{
		ToolName:  "get_task",
		Operation: "get_task",
		RequestID: json.RawMessage(`1`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure == nil {
		t.Fatal("failure = nil")
	}
	if !failure.Retryable || failure.Error != "den_backend_unavailable" {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestClientAuthFailureWithoutBodyHasConfigHint(t *testing.T) {
	failure := statusFailure("den-core", "get_task", "get_task", http.StatusUnauthorized, nil)
	if failure.Retryable {
		t.Fatal("Retryable = true, want false")
	}
	if failure.Error != "den_backend_auth_failed" {
		t.Fatalf("Error = %q, want den_backend_auth_failed", failure.Error)
	}
	if failure.Message == http.StatusText(http.StatusUnauthorized) {
		t.Fatalf("Message = %q, want config hint", failure.Message)
	}
}

func TestFailureTextIncludesToolCircuitAndStatus(t *testing.T) {
	statusCode := http.StatusBadGateway
	failure := Failure{
		Error:        "den_backend_unavailable",
		Retryable:    true,
		Backend:      "den-core",
		Operation:    "get_task",
		Tool:         "get_task",
		Message:      "bad gateway",
		StatusCode:   &statusCode,
		CircuitState: string(StateUnavailable),
	}

	text := failure.Text()
	for _, want := range []string{`"tool":"get_task"`, `"status_code":502`, `"circuit_state":"unavailable"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("Failure.Text() = %s, missing %s", text, want)
		}
	}
}
