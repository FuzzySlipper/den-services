//go:build linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// TestOpsRunKeepsTerminalInputUsable runs den-tool as the foreground job of a
// fresh pseudo-terminal and has the wrapped command read a line, as a backup
// passphrase prompt would.
func TestOpsRunKeepsTerminalInputUsable(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	binary := filepath.Join(t.TempDir(), "den-tool")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build den-tool: %v\n%s", err, output)
	}
	master, terminal := openPTY(t)

	command := exec.Command(binary, "ops", "run", "--job", "tty-test", "--heartbeat", "0",
		"--", "sh", "-c", `read line; echo "got:$line"`)
	command.Env = append(os.Environ(), "DEN_OPS_URL=http://"+freeAddress(t), "DEN_OPS_SPOOL="+t.TempDir())
	command.Stdin, command.Stdout, command.Stderr = terminal, terminal, terminal
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	_ = terminal.Close()

	var mu sync.Mutex
	var output bytes.Buffer
	go func() {
		chunk := make([]byte, 4096)
		for {
			n, err := master.Read(chunk)
			mu.Lock()
			output.Write(chunk[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	time.Sleep(300 * time.Millisecond)
	if _, err := master.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("den-tool exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("den-tool did not finish; the command could not read the terminal. output:\n%s", output.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		seen := output.String()
		mu.Unlock()
		if strings.Contains(seen, "got:hello") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("command output missing the line it read:\n%s", seen)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func openPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal support: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	unlock := int32(0)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		t.Fatalf("unlock pty: %v", errno)
	}
	var number uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&number))); errno != 0 {
		t.Fatalf("pty number: %v", errno)
	}
	terminal, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(number)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}
	return master, terminal
}
