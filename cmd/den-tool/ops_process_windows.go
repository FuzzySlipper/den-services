//go:build windows

package main

import (
	"os"
	"os/exec"
)

func ownProcessGroup(*exec.Cmd) {}

// signalProcessTree stops the command. Windows cannot deliver INT/TERM to
// another console process, so cancellation ends it outright.
func signalProcessTree(command *exec.Cmd, _ os.Signal) {
	_ = command.Process.Kill()
}

func signalExitCode(*exec.Cmd) (int, bool) { return 0, false }
