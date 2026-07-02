package broker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type ManagedProcess struct {
	Command *exec.Cmd
	PID     int
}

func startManagedProcess(ctx context.Context, command string, workDir string, env map[string]string, stdoutPath string, stderrPath string) (*ManagedProcess, error) {
	stdout, err := os.Create(stdoutPath)
	if err != nil {
		return nil, fmt.Errorf("creating server stdout log: %w", err)
	}
	defer stdout.Close()
	stderr, err := os.Create(stderrPath)
	if err != nil {
		return nil, fmt.Errorf("creating server stderr log: %w", err)
	}
	defer stderr.Close()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = workDir
	cmd.Env = mergeEnv(os.Environ(), env)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting dev server command: %w", err)
	}
	return &ManagedProcess{Command: cmd, PID: cmd.Process.Pid}, nil
}

func stopProcessGroup(pid int, timeout time.Duration) error {
	if pid <= 0 {
		return nil
	}
	if !processAlive(pid) {
		return nil
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("killing process group %d: %w", pid, err)
	}
	return nil
}

func stopManagedProcess(process *ManagedProcess, timeout time.Duration) error {
	if process == nil || process.Command == nil {
		return nil
	}
	if processAlive(process.PID) {
		_ = syscall.Kill(-process.PID, syscall.SIGTERM)
	}
	done := make(chan error, 1)
	go func() {
		done <- process.Command.Wait()
	}()
	select {
	case err := <-done:
		return normalizeWaitError(process.PID, err)
	case <-time.After(timeout):
		_ = syscall.Kill(-process.PID, syscall.SIGKILL)
	}
	select {
	case err := <-done:
		return normalizeWaitError(process.PID, err)
	case <-time.After(timeout):
		return fmt.Errorf("waiting for managed process %d timed out", process.PID)
	}
}

func normalizeWaitError(pid int, err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}
	return fmt.Errorf("waiting for managed process %d: %w", pid, err)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func mergeEnv(base []string, values map[string]string) []string {
	merged := append([]string(nil), base...)
	for key, value := range values {
		merged = append(merged, key+"="+value)
	}
	return merged
}
