package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoveryCommandsExposeAvailabilityInJSONAndHumanSearch(t *testing.T) {
	t.Setenv("DEN_REPO_ROOT", filepath.Dir(repositoryRootForTest(t)))
	for _, args := range [][]string{
		{"list", "--json"},
		{"search", "board", "--json"},
		{"describe", "board.search", "--json"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runCLI(args, &stdout, &stderr); code != 0 {
			t.Fatalf("runCLI(%v) code = %d, stderr = %q", args, code, stderr.String())
		}
		var value any
		if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
			t.Fatalf("runCLI(%v) JSON: %v", args, err)
		}
		encoded := stdout.String()
		if !strings.Contains(encoded, `"availability"`) || !strings.Contains(encoded, `"reason"`) {
			t.Fatalf("runCLI(%v) omitted availability readback: %s", args, encoded)
		}
	}

	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"search", "board"}, &stdout, &stderr); code != 0 {
		t.Fatalf("human search code = %d, stderr = %q", code, stderr.String())
	}
	if output := stdout.String(); !strings.Contains(output, "unavailable:") && !strings.Contains(output, "available") {
		t.Fatalf("human search omitted availability: %q", output)
	}
}

func TestToolReadbackExplainsMissingWorkingDirectoryAndExecutable(t *testing.T) {
	missingDirectory := testTool("missing-directory", RiskRead)
	missingDirectory.WorkingDirectory = filepath.Join(t.TempDir(), "absent")
	readback := newToolReadback(missingDirectory)
	if readback.Availability != "unavailable" || !strings.Contains(readback.Reason, "working directory") {
		t.Fatalf("missing directory readback = %#v", readback)
	}

	directory := t.TempDir()
	missingExecutable := testTool("missing-executable", RiskRead)
	missingExecutable.WorkingDirectory = directory
	missingExecutable.Argv = []string{"den-tool-command-that-does-not-exist"}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", directory)
	readback = newToolReadback(missingExecutable)
	t.Setenv("PATH", originalPath)
	if readback.Availability != "unavailable" || !strings.Contains(readback.Reason, "executable") {
		t.Fatalf("missing executable readback = %#v", readback)
	}
}

func TestBoardCLIRejectsUnknownCommandsAndIrrelevantFlagsBeforeCallingService(t *testing.T) {
	for _, args := range [][]string{
		{"board", "missing"},
		{"board", "get-post", "--post-id", "1", "--actor", "irrelevant"},
		{"board", "list-posts", "--project", "den-services", "--limit", "101"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runCLI(args, &stdout, &stderr); code != 2 {
			t.Errorf("runCLI(%v) code = %d, want usage error; stderr=%q", args, code, stderr.String())
		}
	}
}
