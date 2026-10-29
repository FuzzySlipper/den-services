//go:build !windows

package main

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"unsafe"
)

// ownProcessGroup starts the command as the leader of a new process group so
// a cancellation reaches everything it launched, not just a wrapper shell.
// When den-tool runs in the foreground of its terminal, the new group takes
// over the terminal so prompts (a passphrase, a confirmation) still read
// input; the returned function hands the terminal back afterwards.
func ownProcessGroup(command *exec.Cmd) func() {
	attributes := &syscall.SysProcAttr{Setpgid: true}
	restore := func() {}
	terminal := int(os.Stdin.Fd())
	if group, ok := terminalForegroundGroup(terminal); ok && group == syscall.Getpgrp() {
		attributes.Foreground, attributes.Ctty = true, terminal
		restore = func() { reclaimTerminal(terminal, group) }
	}
	command.SysProcAttr = attributes
	return restore
}

// terminalForegroundGroup reports the foreground process group of fd when fd
// is this process's controlling terminal.
func terminalForegroundGroup(fd int) (int, bool) {
	var group int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCGPGRP), uintptr(unsafe.Pointer(&group)))
	return int(group), errno == 0
}

// reclaimTerminal makes group the foreground again. den-tool is a background
// process at this point, so SIGTTOU is ignored for the call.
func reclaimTerminal(fd int, group int) {
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)
	value := int32(group)
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCSPGRP), uintptr(unsafe.Pointer(&value)))
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
