//go:build !windows

package main

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestOpsRunCancellationReachesBackupChildren stops den-tool while a wrapper
// shell waits on a backup child, as a real script-launched backup would.
func TestOpsRunCancellationReachesBackupChildren(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	binary := filepath.Join(t.TempDir(), "den-tool")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build den-tool: %v\n%s", err, output)
	}
	spoolDir := t.TempDir()
	command := exec.Command(binary, "ops", "run", "--job", "signal-test", "--heartbeat", "0",
		"--", "sh", "-c", `sleep 60 & child=$!; echo "$child"; wait "$child"`)
	command.Env = append(os.Environ(), "DEN_OPS_URL=http://"+freeAddress(t), "DEN_OPS_SPOOL="+spoolDir)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("reading child pid: %v", err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("child pid %q: %v", line, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })

	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 128+int(syscall.SIGTERM) {
			t.Fatalf("den-tool exit = %v, want %d", err, 128+int(syscall.SIGTERM))
		}
	case <-time.After(15 * time.Second):
		t.Fatal("den-tool kept waiting after SIGTERM")
	}
	for deadline := time.Now().Add(5 * time.Second); processAlive(child); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("backup child %d survived cancellation", child)
		}
	}
	spooled, _ := filepath.Glob(filepath.Join(spoolDir, "*.json"))
	if len(spooled) != 1 {
		t.Fatalf("terminal report not spooled: %v", spooled)
	}
	report, err := os.ReadFile(spooled[0])
	if err != nil || !strings.Contains(string(report), `"state":"failed"`) || !strings.Contains(string(report), `"exit_code":143`) {
		t.Fatalf("spooled report = %s, %v", report, err)
	}
}

// processAlive treats a zombie as gone: it has exited and only awaits reaping.
func processAlive(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return syscall.Kill(pid, 0) == nil
	}
	fields := strings.Fields(string(data))
	return len(fields) > 2 && fields[2] != "Z"
}
