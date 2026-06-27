package backend

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
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

func TestClientNegotiatesStreamableMCPSessionOnDemand(t *testing.T) {
	var sawInitialAccept bool
	var sawSessionHeader bool
	var sawTool string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			sawInitialAccept = true
		}
		var request struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if request.Method == "initialize" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Mcp-Session-Id", "session-1")
			_, _ = w.Write([]byte("event: message\n"))
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":"den-services-mcp-backend-init","result":{"protocolVersion":"2025-11-25"}}` + "\n\n"))
			return
		}
		if request.Method != "tools/call" {
			t.Fatalf("method = %q, want tools/call", request.Method)
		}
		if r.Header.Get("Mcp-Session-Id") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":-32000,"message":"Bad Request: A new session can only be created by an initialize request. Include a valid Mcp-Session-Id header for non-initialize requests."},"id":"","jsonrpc":"2.0"}`))
			return
		}
		if r.Header.Get("Mcp-Session-Id") == "session-1" {
			sawSessionHeader = true
		}
		sawTool = request.Params.Name
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\n"))
		_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}],"isError":false}}` + "\n\n"))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	route := testRoute("get_task", "den-core")
	route.Path = "/mcp"
	result, failure, err := client.Call(context.Background(), testBackend("den-core", server.URL), route, ToolCall{
		ToolName:  "get_task",
		Operation: "get_task",
		RequestID: json.RawMessage(`1`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if failure != nil {
		t.Fatalf("Call() failure = %#v", failure)
	}
	if !sawInitialAccept {
		t.Fatal("initial request did not include streamable MCP Accept header")
	}
	if !sawSessionHeader {
		t.Fatal("retried tools/call did not include negotiated Mcp-Session-Id")
	}
	if sawTool != "get_task" {
		t.Fatalf("tool = %q, want get_task", sawTool)
	}
	if !strings.Contains(string(result.Value), `"text":"ok"`) {
		t.Fatalf("result = %s", result.Value)
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
