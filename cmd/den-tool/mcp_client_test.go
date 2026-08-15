package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPClientCallsTypedOperationAndBoundsResponse(t *testing.T) {
	var requestBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		requestBody, _ = io.ReadAll(request.Body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":"den-tool","result":{"content":[{"type":"text","text":"ok"}],"isError":false,"structuredContent":{"id":7011}}}`)
	}))
	defer server.Close()
	client, err := NewMCPClient(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, isError, err := client.Call(context.Background(), "get_task", json.RawMessage(`{"task_id":7011}`))
	if err != nil || isError {
		t.Fatalf("Call error = %v, isError = %t", err, isError)
	}
	if !bytes.Contains(result, []byte(`"id":7011`)) {
		t.Fatalf("result = %s", result)
	}
	var rpc map[string]any
	if err := json.Unmarshal(requestBody, &rpc); err != nil {
		t.Fatal(err)
	}
	params := rpc["params"].(map[string]any)
	if params["name"] != "get_task" || params["arguments"].(map[string]any)["task_id"] != float64(7011) {
		t.Fatalf("request = %#v", rpc)
	}

	largeServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, strings.Repeat("x", maxMCPResponseBytes+1))
	}))
	defer largeServer.Close()
	largeClient, _ := NewMCPClient(largeServer.URL, "", largeServer.Client())
	if _, _, err := largeClient.Call(context.Background(), "get_task", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestDenCLIInvokesCatalogOperation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		if !bytes.Contains(body, []byte(`"name":"get_task"`)) || !bytes.Contains(body, []byte(`"task_id":7011`)) {
			t.Errorf("request body = %s", body)
		}
		_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":"den-tool","result":{"isError":false,"structuredContent":{"id":7011}}}`)
	}))
	defer server.Close()
	t.Setenv("DEN_MCP_URL", server.URL)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := runCLI([]string{"den", "get_task", "--task-id", "7011"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"id":7011`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}
