package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// runResourcesRun is "den-tool resources run": check in, heartbeat while the
// command runs, then check out with completed, failed or cancelled. The
// command's output and exit status pass through unchanged, and reporting
// problems only warn: the work always runs.
func runResourcesRun(args []string, client *ResourcesClient, stderr io.Writer) int {
	flags, err := parseResourcesFlags(args, workFlagNames, nil)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "den-tool resources: %v\n", err)
		return 2
	}
	command := append(flags.positional, flags.command...)
	if len(command) == 0 {
		_, _ = fmt.Fprintln(stderr, "den-tool resources: provide a command after --")
		return 2
	}
	report, err := workReport(flags, false)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "den-tool resources: %v\n", err)
		return 2
	}
	// A stable reporter key lets a retried check-in find the same row after
	// an uncertain HTTP response.
	report["source"], report["external_id"] = "command", randomID()
	ttl, _ := report["ttl_seconds"].(float64)
	interval := min(30*time.Second, time.Duration(ttl/3*float64(time.Second)))
	interval = max(interval, time.Second)

	ctx := context.Background()
	reporter := &workReporter{client: client, stderr: stderr}
	reporter.checkIn(ctx, report)
	child := exec.Command(command[0], command[1:]...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	exitCode, runErr := runForwardingSignals(child, interval, func(time.Time) { reporter.checkIn(ctx, report) })
	if runErr != nil {
		_, _ = fmt.Fprintf(stderr, "den-tool resources: %v\n", runErr)
	}
	state := "failed"
	switch {
	case runErr == nil && exitCode == 0:
		state = "completed"
	case runErr == nil:
		if _, signalled := signalExitCode(child); signalled {
			state = "cancelled"
		}
	}
	if reporter.id != "" {
		note := fmt.Sprintf("Command exited with status %d. %v", exitCode, report["note"])
		if _, err := postJSON(ctx, client, resourcesPath("work", reporter.id), map[string]any{"state": state, "note": strings.TrimSpace(note)}); err != nil {
			_, _ = fmt.Fprintf(stderr, "resource-board: check-out could not be saved (%v); the check-in will become stale\n", err)
		}
	}
	return exitCode
}

// workReporter checks in once and then renews the row, warning once per
// outage instead of on every heartbeat.
type workReporter struct {
	client  *ResourcesClient
	stderr  io.Writer
	id      string
	failing bool
}

func (r *workReporter) checkIn(ctx context.Context, report map[string]any) {
	var body []byte
	var err error
	if r.id == "" {
		body, err = postJSON(ctx, r.client, resourcesPath("work"), report)
	} else {
		body, err = r.client.Do(ctx, http.MethodPost, resourcesPath("work", r.id), []byte(`{}`))
	}
	if err != nil {
		if !r.failing {
			_, _ = fmt.Fprintf(r.stderr, "resource-board: reporting unavailable (%v); command continues, reporting will retry\n", err)
		}
		r.failing = true
		return
	}
	var receipt struct {
		ID       string   `json:"id"`
		Warnings []string `json:"warnings"`
	}
	if json.Unmarshal(body, &receipt) == nil {
		if r.id == "" && !r.failing {
			for _, warning := range receipt.Warnings {
				_, _ = fmt.Fprintf(r.stderr, "resource-board: %s\n", warning)
			}
		}
		if receipt.ID != "" {
			r.id = receipt.ID
		}
	}
	r.failing = false
}

func randomID() string {
	buffer := make([]byte, 16)
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
