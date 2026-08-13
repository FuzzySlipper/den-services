package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func RunTool(ctx context.Context, tool Tool, stdout, stderr io.Writer, extraArgs ...string) error {
	if len(tool.Argv) == 0 {
		return fmt.Errorf("tool %q has no declared argv", tool.ID)
	}
	if tool.WorkingDirectory == "" {
		return fmt.Errorf("tool %q has no declared working directory", tool.ID)
	}
	if info, err := os.Stat(tool.WorkingDirectory); err != nil {
		return fmt.Errorf("tool %q working directory %q is unavailable: %w", tool.ID, tool.WorkingDirectory, err)
	} else if !info.IsDir() {
		return fmt.Errorf("tool %q working directory %q is not a directory", tool.ID, tool.WorkingDirectory)
	}
	if _, err := exec.LookPath(tool.Argv[0]); err != nil {
		return fmt.Errorf("tool %q executable %q is unavailable: %w", tool.ID, tool.Argv[0], err)
	}

	argv := make([]string, 0, len(tool.Argv)+len(extraArgs))
	argv = append(argv, tool.Argv...)
	argv = append(argv, extraArgs...)
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Dir = tool.WorkingDirectory
	command.Stdin = os.Stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}
