package main

import (
	"bytes"
	"reflect"
	"testing"
)

func TestBoardRelayCommandsUseMCPTransport(t *testing.T) {
	syncFlags, err := parseBoardFlags("github-sync", []string{"--project", "rusty-engine"})
	if err != nil {
		t.Fatal(err)
	}
	operation, arguments, err := boardMCPCall("github-sync", syncFlags)
	if err != nil {
		t.Fatal(err)
	}
	if operation != "sync_board_github" || !reflect.DeepEqual(arguments, map[string]any{"project_id": "rusty-engine"}) {
		t.Fatalf("sync call = %q %#v", operation, arguments)
	}
	visibilityFlags, err := parseBoardFlags("github-visibility", []string{"--visibility", "private"})
	if err != nil {
		t.Fatal(err)
	}
	operation, arguments, err = boardMCPCall("github-visibility", visibilityFlags)
	if err != nil {
		t.Fatal(err)
	}
	if operation != "set_board_github_visibility" || !reflect.DeepEqual(arguments, map[string]any{"visibility": "private"}) {
		t.Fatalf("visibility call = %q %#v", operation, arguments)
	}
}

func TestBoardRelayCommandsIgnoreDirectBoardOverride(t *testing.T) {
	t.Setenv("DEN_BOARD_URL", "://invalid-direct-board-url")
	t.Setenv("DEN_MCP_URL", "://invalid-mcp-url")
	var stdout, stderr bytes.Buffer
	if code := runBoardCommand([]string{"github-sync", "--project", "rusty-engine"}, &stdout, &stderr); code == 0 {
		t.Fatal("relay command unexpectedly succeeded")
	}
	if got := stderr.String(); !bytes.Contains([]byte(got), []byte("MCP URL")) {
		t.Fatalf("stderr = %q, want MCP transport error", got)
	}
}
