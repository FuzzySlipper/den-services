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
	report.Source, report.ExternalID = "command", randomID()
	child := exec.Command(command[0], command[1:]...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	return runReportedCommand(context.Background(), child, client, report, stderr, nil)
}

func runReportedCommand(ctx context.Context, child *exec.Cmd, client *ResourcesClient, report resourceWorkReport, stderr io.Writer, cancelled func(int) bool) int {
	interval := min(30*time.Second, time.Duration(report.TTLSeconds/3*float64(time.Second)))
	interval = max(interval, time.Second)
	reporter := &workReporter{client: client, stderr: stderr}
	// The report's external ID lets a retry find the same row after an
	// uncertain response.
	reporter.checkIn(ctx, report)
	exitCode, runErr := runForwardingSignals(child, interval, func(time.Time) { reporter.checkIn(ctx, report) })
	if runErr != nil {
		_, _ = fmt.Fprintf(stderr, "den-tool resources: %v\n", runErr)
	}
	state := "failed"
	switch {
	case runErr == nil && exitCode == 0:
		state = "completed"
	case ctx.Err() != nil:
		state = "cancelled"
		if runErr != nil {
			exitCode = 130
		}
	case cancelled != nil && cancelled(exitCode):
		state = "cancelled"
	case runErr == nil:
		if _, signalled := signalExitCode(child); signalled {
			state = "cancelled"
		}
	}
	if reporter.id != "" {
		note := fmt.Sprintf("Command exited with status %d. %s", exitCode, report.Note)
		if _, err := postJSON(context.WithoutCancel(ctx), client, resourcesPath("work", reporter.id), map[string]any{"state": state, "note": strings.TrimSpace(note)}); err != nil {
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

func (r *workReporter) checkIn(ctx context.Context, report resourceWorkReport) {
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
