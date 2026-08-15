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

func TestDenCLIInvokesLongTailReadWriteAndDestructiveOperations(t *testing.T) {
	var operations []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var rpc map[string]any
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil {
			t.Fatal(err)
		}
		params := rpc["params"].(map[string]any)
		operations = append(operations, params["name"].(string))
		_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":"den-tool","result":{"isError":false,"structuredContent":{"accepted":true}}}`)
	}))
	defer server.Close()
	t.Setenv("DEN_MCP_URL", server.URL)

	for _, args := range [][]string{
		{"den", "get_project", "--project-id", "den-services"},
		{"den", "add_dependency", "--task-id", "2", "--depends-on", "1"},
		{"den", "purge_board_post", "--post-id", "1", "--actor-identity", "test", "--reason", "test"},
	} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if code := runCLI(args, &stdout, &stderr); code != 0 {
			t.Fatalf("runCLI(%v) code = %d, stderr = %s", args, code, stderr.String())
		}
	}
	want := []string{"get_project", "add_dependency", "purge_board_post"}
	if strings.Join(operations, ",") != strings.Join(want, ",") {
		t.Fatalf("operations = %v, want %v", operations, want)
	}
}

func TestBoardCLIDefaultsToMCPWithoutBoardInfrastructureConfiguration(t *testing.T) {
	var calls []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer agent-token" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		var rpc map[string]any
		if err := json.NewDecoder(request.Body).Decode(&rpc); err != nil {
			t.Fatal(err)
		}
		params := rpc["params"].(map[string]any)
		calls = append(calls, params)
		name := params["name"].(string)
		structured := `{"id":42}`
		if name == "search_board_posts" {
			structured = `{"results":[{"post_id":42}]}`
		}
		_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":"den-tool","result":{"isError":false,"structuredContent":`+structured+`}}`)
	}))
	defer server.Close()
	t.Setenv("DEN_BOARD_URL", "")
	t.Setenv("DEN_MCP_URL", server.URL)
	t.Setenv("DEN_MCP_TOKEN", "agent-token")

	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"board", "create-post", "--project", "den-services", "--title", "Topic", "--body", "Body", "--author", "agent", "--json"}, `{"id":42}`},
		{[]string{"board", "search", "--project", "den-services", "--query", "Topic", "--limit", "5", "--json"}, `{"results":[{"post_id":42}]}`},
	} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if code := runCLI(test.args, &stdout, &stderr); code != 0 {
			t.Fatalf("runCLI(%v) code = %d, stderr = %s", test.args, code, stderr.String())
		}
		if strings.TrimSpace(stdout.String()) != test.want {
			t.Fatalf("runCLI(%v) stdout = %q, want %q", test.args, stdout.String(), test.want)
		}
	}
	if len(calls) != 2 || calls[0]["name"] != "create_board_post" || calls[1]["name"] != "search_board_posts" {
		t.Fatalf("calls = %#v", calls)
	}
	createArguments := calls[0]["arguments"].(map[string]any)
	if createArguments["project_id"] != "den-services" || createArguments["body_markdown"] != "Body" {
		t.Fatalf("create arguments = %#v", createArguments)
	}
	searchArguments := calls[1]["arguments"].(map[string]any)
	if searchArguments["query"] != "Topic" || searchArguments["limit"] != float64(5) {
		t.Fatalf("search arguments = %#v", searchArguments)
	}
}
