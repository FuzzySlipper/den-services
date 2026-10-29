package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// stderrTailBytes is how much of the command's stderr goes into a failure or
// warning report; the full stream still reaches the caller unchanged.
const stderrTailBytes = 8 << 10

const processOutputGrace = 5 * time.Second

type opsRunOptions struct {
	jobID           string
	runID           string
	host            string
	engine          string
	runner          string
	kind            string
	target          string
	logPath         string
	recoveryPointer string
	heartbeat       time.Duration
	warningCodes    []int
	command         []string
}

// opsRunReport is the report body den-tool sends; field names follow the Ops
// run report contract (docs/ops-api-contract.md).
type opsRunReport struct {
	SchemaVersion   int        `json:"schema_version"`
	JobID           string     `json:"job_id"`
	RunID           string     `json:"run_id"`
	Host            string     `json:"host"`
	Engine          string     `json:"engine,omitempty"`
	Runner          string     `json:"runner"`
	Kind            string     `json:"kind"`
	State           string     `json:"state"`
	Phase           string     `json:"phase,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	ExitCode        *int       `json:"exit_code,omitempty"`
	Error           string     `json:"error,omitempty"`
	Warnings        []string   `json:"warnings"`
	Target          string     `json:"target,omitempty"`
	LogPath         string     `json:"log_path,omitempty"`
	RecoveryPointer string     `json:"recovery_pointer,omitempty"`
	Command         []string   `json:"command"`
}

func parseOpsRunArgs(args []string) (opsRunOptions, error) {
	separator := slices.Index(args, "--")
	if separator < 0 || separator == len(args)-1 {
		return opsRunOptions{}, errors.New("ops run needs -- followed by the command to run")
	}
	host, _ := os.Hostname()
	host, _, _ = strings.Cut(host, ".")
	var options opsRunOptions
	var warningCodes string
	flags := flag.NewFlagSet("ops run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.jobID, "job", "", "stable job ID (required)")
	flags.StringVar(&options.runID, "run-id", "", "stable run ID (default: UTC start time and PID)")
	flags.StringVar(&options.host, "host", host, "reporting host")
	flags.StringVar(&options.engine, "engine", "", "backup engine, e.g. borg")
	flags.StringVar(&options.runner, "runner", "den-tool", "what launched the command")
	flags.StringVar(&options.kind, "kind", "backup", "backup or maintenance")
	flags.StringVar(&options.target, "target", "", "where the backup is written")
	flags.StringVar(&options.logPath, "log-path", "", "where the command's full log is kept")
	flags.StringVar(&options.recoveryPointer, "recovery-pointer", "", "where recovery instructions live")
	flags.DurationVar(&options.heartbeat, "heartbeat", time.Minute, "heartbeat interval while running (0 disables)")
	flags.StringVar(&warningCodes, "warning-exit-codes", "", "comma-separated exit codes that mean warning (default 1 for --engine borg)")
	if err := flags.Parse(args[:separator]); err != nil {
		return opsRunOptions{}, err
	}
	if flags.NArg() != 0 {
		return opsRunOptions{}, fmt.Errorf("unexpected argument %q before --", flags.Arg(0))
	}
	if options.jobID == "" {
		return opsRunOptions{}, errors.New("ops run needs --job")
	}
	if options.heartbeat < 0 {
		return opsRunOptions{}, errors.New("--heartbeat must not be negative")
	}
	if warningCodes == "" && options.engine == "borg" {
		// Borg exit 1 means the archive was made but something needs a look.
		warningCodes = "1"
	}
	for _, value := range strings.Split(warningCodes, ",") {
		if strings.TrimSpace(value) == "" {
			continue
		}
		code, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || code <= 0 {
			return opsRunOptions{}, fmt.Errorf("--warning-exit-codes entry %q must be a positive integer", value)
		}
		options.warningCodes = append(options.warningCodes, code)
	}
	options.command = args[separator+1:]
	return options, nil
}

// runOpsRun runs a command as a job run and reports it to Ops. The command's
// stdin/stdout/stderr pass through and its exit status is returned unchanged;
// reporting problems only produce warnings on stderr.
func runOpsRun(args []string, _ io.Writer, stderr io.Writer) int {
	options, err := parseOpsRunArgs(args)
	if err != nil {
		return opsUsageError(stderr, err.Error())
	}
	client, clientErr := OpsClientFromEnv()
	spool, spoolErr := OpsSpoolFromEnv()
	reporting := clientErr == nil && spoolErr == nil
	if !reporting {
		_, _ = fmt.Fprintf(stderr, "den-tool: warning: ops reporting disabled: %v\n", errors.Join(clientErr, spoolErr))
	}
	ctx := context.Background()
	if reporting {
		replayQuietly(ctx, client, spool, stderr)
	}

	started := time.Now().UTC()
	if options.runID == "" {
		options.runID = fmt.Sprintf("%s-%d", started.Format("20060102T150405Z"), os.Getpid())
	}
	report := opsRunReport{
		SchemaVersion: 1, JobID: options.jobID, RunID: options.runID, Host: options.host, Engine: options.engine,
		Runner: options.runner, Kind: options.kind, State: "running", StartedAt: started, UpdatedAt: started,
		Warnings: []string{}, Target: options.target, LogPath: options.logPath,
		RecoveryPointer: options.recoveryPointer, Command: options.command,
	}
	path := opsPath("jobs", options.jobID, "runs", options.runID)
	heartbeatWarned := false
	sendHeartbeat := func(at time.Time) {
		if !reporting {
			return
		}
		heartbeat := report
		heartbeat.UpdatedAt = at
		body, err := json.Marshal(heartbeat)
		if err == nil {
			// Heartbeats are not spooled: the terminal report supersedes them.
			_, err = client.Do(ctx, "PUT", path, body)
		}
		if err != nil && !heartbeatWarned {
			heartbeatWarned = true
			_, _ = fmt.Fprintf(stderr, "den-tool: warning: ops heartbeats for %s not delivered (reported once): %v\n", options.runID, err)
		}
	}
	sendHeartbeat(started)

	tail := &tailBuffer{limit: stderrTailBytes}
	command := exec.Command(options.command[0], options.command[1:]...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, io.MultiWriter(os.Stderr, tail)
	exitCode, runErr := runForwardingSignals(command, options.heartbeat, sendHeartbeat)

	finished := time.Now().UTC()
	report.UpdatedAt, report.FinishedAt, report.ExitCode = finished, &finished, &exitCode
	switch {
	case runErr != nil:
		report.State, report.Error = "failed", runErr.Error()
	case exitCode == 0:
		report.State = "success"
	case slices.Contains(options.warningCodes, exitCode):
		report.State = "warning"
		report.Warnings = []string{fmt.Sprintf("exit status %d: %s", exitCode, tail.String())}
	default:
		report.State, report.Error = "failed", fmt.Sprintf("exit status %d: %s", exitCode, tail.String())
	}
	if reporting {
		body, err := json.Marshal(report)
		if err == nil {
			_, err = deliverOrSpool(ctx, client, spool, "PUT", path, body, finished, stderr)
		}
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "den-tool: warning: ops report for %s not recorded: %v\n", options.runID, err)
		}
	}
	return exitCode
}

// runForwardingSignals starts the command, passes INT/TERM through to it and
// calls heartbeat periodically until it exits. The returned code is what a
// shell would report (128+signal when the command was killed).
func runForwardingSignals(command *exec.Cmd, interval time.Duration, heartbeat func(time.Time)) (int, error) {
	restoreTerminal := ownProcessGroup(command)
	defer restoreTerminal()
	// A process left behind by the command can hold its stderr pipe open;
	// stop waiting for that output shortly after the command itself exits.
	command.WaitDelay = processOutputGrace
	if err := command.Start(); err != nil {
		return 127, fmt.Errorf("start %s: %w", command.Path, err)
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var ticks <-chan time.Time
	if interval > 0 {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		ticks = ticker.C
	}
	for {
		select {
		case received := <-signals:
			signalProcessTree(command, received)
		case at := <-ticks:
			heartbeat(at.UTC())
		case err := <-done:
			return exitCodeOf(command, err), nil
		}
	}
}

func exitCodeOf(command *exec.Cmd, err error) int {
	if err == nil {
		return 0
	}
	if code, ok := signalExitCode(command); ok {
		return code
	}
	if code := command.ProcessState.ExitCode(); code >= 0 {
		return code
	}
	return 1
}

// tailBuffer keeps the last limit bytes written to it.
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.data = b.data[len(b.data)-b.limit:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}
