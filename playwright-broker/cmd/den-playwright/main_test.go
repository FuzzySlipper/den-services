package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunProjectDoesNotPrintEmptyEvidenceWhenSetupFails(t *testing.T) {
	stdout := captureStdout(t, func() {
		err := run([]string{"run", "missing", "-config", "/path/that/does/not/exist.yaml"})
		if err == nil {
			t.Fatal("run() error = nil, want config error")
		}
	})
	if strings.Contains(stdout, "evidence=\n") || strings.Contains(stdout, "base_url=\n") {
		t.Fatalf("stdout included empty evidence fields: %q", stdout)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = original
	}()
	fn()
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	var output bytes.Buffer
	if _, err := io.Copy(&output, reader); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	return output.String()
}
