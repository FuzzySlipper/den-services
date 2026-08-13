package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunToolUsesDeclaredCwdAndForwardsExactArguments(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "capture-args.sh")
	scriptBody := "#!/bin/sh\nprintf 'cwd=%s\\n' \"$(pwd)\"\nprintf 'argc=%s\\n' \"$#\"\nfor argument in \"$@\"; do printf 'arg=<%s>\\n' \"$argument\"; done\n"
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatalf("write helper: %v", err)
	}

	tool := Tool{ID: "test.capture", WorkingDirectory: directory, Argv: []string{script, "declared"}}
	var stdout, stderr bytes.Buffer
	err := RunTool(context.Background(), tool, &stdout, &stderr, "extra value", "$literal", "--flag")
	if err != nil {
		t.Fatalf("RunTool() error = %v", err)
	}
	want := strings.Join([]string{
		"cwd=" + directory,
		"argc=4",
		"arg=<declared>",
		"arg=<extra value>",
		"arg=<$literal>",
		"arg=<--flag>",
		"",
	}, "\n")
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunToolPreservesExitCodeAndReportsMissingExecutable(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "exit.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 19\n"), 0o755); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	tool := Tool{ID: "test.exit", WorkingDirectory: directory, Argv: []string{script}}
	err := RunTool(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{})
	var exitError interface{ ExitCode() int }
	if !errors.As(err, &exitError) {
		t.Fatalf("RunTool() error = %v, want exit error", err)
	}
	if got := exitError.ExitCode(); got != 19 {
		t.Fatalf("exit code = %d, want 19", got)
	}

	missing := Tool{ID: "test.missing", WorkingDirectory: directory, Argv: []string{"den-tool-test-command-that-does-not-exist"}}
	if err := RunTool(context.Background(), missing, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("missing executable error = %v, want clear executable error", err)
	}
}
