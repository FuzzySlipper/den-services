package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolUsageReportCountsStructuredEvents(t *testing.T) {
	journalctl := filepath.Join(t.TempDir(), "journalctl")
	fixture := `#!/usr/bin/env bash
echo '{"msg":"mcp_tool_call","requested_tool":"task_get","canonical_tool":"get_task","backend":"tasks","outcome":"success","retryable":false}'
echo '{"msg":"mcp_tool_call","requested_tool":"task_get","canonical_tool":"get_task","backend":"tasks","outcome":"success","retryable":false}'
echo '{"msg":"unrelated"}'
`
	if err := os.WriteFile(journalctl, []byte(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", "../../scripts/tool_usage_report.sh", "--since", "1 hour ago")
	command.Env = append(os.Environ(), "JOURNALCTL_BIN="+journalctl)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("tool_usage_report.sh error = %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "2\ttask_get\tget_task\ttasks\tsuccess\tfalse") {
		t.Fatalf("report output = %s", output)
	}
}
