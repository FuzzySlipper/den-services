//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// ownProcessGroup starts the command as the leader of a new process group so
// a cancellation reaches everything it launched, not just a wrapper shell.
func ownProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalProcessTree(command *exec.Cmd, received os.Signal) {
	signalNumber, ok := received.(syscall.Signal)
	if !ok {
		signalNumber = syscall.SIGTERM
	}
	if err := syscall.Kill(-command.Process.Pid, signalNumber); err != nil {
		_ = command.Process.Signal(received)
	}
}

func signalExitCode(command *exec.Cmd) (int, bool) {
	status, ok := command.ProcessState.Sys().(syscall.WaitStatus)
	if ok && status.Signaled() {
		return 128 + int(status.Signal()), true
	}
	return 0, false
}
