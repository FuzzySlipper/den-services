package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

const opsUsage = `usage:
  den-tool ops status [--json]
  den-tool ops job <job_id> [--limit N] [--json]
  den-tool ops errors [--limit N] [--json]
  den-tool ops report <report.json|->
  den-tool ops verification <job_id> <verification_id> <report.json|->
  den-tool ops register-job <job_id> <job.json|->
  den-tool ops register-host <host_id> <host.json|->
  den-tool ops replay [--json]
  den-tool ops run --job <job_id> [run flags] -- <command> [args...]
  den-tool ops collect [--config FILE] [--dry-run] [--resend] [--json]

DEN_OPS_URL overrides the Ops base URL (default ` + defaultOpsURL + `).
DEN_OPS_SPOOL overrides where undeliverable reports wait for replay.
DEN_OPS_COLLECT_CONFIG overrides the collect config
(default ~/.config/den-ops-collect/config.json).`

type opsArgs struct {
	positional []string
	json       bool
	limit      int
}

func handleOps(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		_, _ = fmt.Fprintln(stderr, opsUsage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "run":
		return runOpsRun(args[1:], stdout, stderr)
	case "collect":
		return runOpsCollect(args[1:], stdout, stderr)
	}
	parsed, err := parseOpsArgs(args[1:])
	if err != nil {
		return opsUsageError(stderr, err.Error())
	}
	client, err := OpsClientFromEnv()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	ctx := context.Background()
	switch args[0] {
	case "status":
		return opsRead(ctx, client, parsed, 0, "/v1/ops/summary", writeOpsSummary, stdout, stderr)
	case "job":
		return opsRead(ctx, client, parsed, 1, opsPathWithLimit(parsed, "jobs", firstOr(parsed.positional)), writeOpsJob, stdout, stderr)
	case "errors":
		return opsRead(ctx, client, parsed, 0, opsPathWithLimit(parsed, "errors"), writeOpsErrors, stdout, stderr)
	case "report":
		return opsWrite(ctx, client, parsed, 1, http.MethodPost, func(p []string) string { return "/v1/ops/reports" }, true, stdout, stderr)
	case "verification":
		return opsWrite(ctx, client, parsed, 3, http.MethodPut, func(p []string) string { return opsPath("jobs", p[0], "verifications", p[1]) }, true, stdout, stderr)
	case "register-job":
		return opsWrite(ctx, client, parsed, 2, http.MethodPut, func(p []string) string { return opsPath("jobs", p[0]) }, false, stdout, stderr)
	case "register-host":
		return opsWrite(ctx, client, parsed, 2, http.MethodPut, func(p []string) string { return opsPath("hosts", p[0]) }, false, stdout, stderr)
	case "replay":
		if len(parsed.positional) != 0 {
			return opsUsageError(stderr, "replay takes no arguments")
		}
		spool, err := OpsSpoolFromEnv()
		if err != nil {
			return writeRuntimeError(stderr, err)
		}
		result := spool.Replay(ctx, client)
		if parsed.json {
			writeJSON(stdout, result)
		} else {
			_, _ = fmt.Fprintf(stdout, "delivered %d, pending %d, rejected %d (%s)\n", result.Delivered, result.Pending, len(result.Rejected), spool.Dir())
		}
		if result.Error != "" {
			_, _ = fmt.Fprintf(stderr, "den-tool: ops replay stopped: %s\n", result.Error)
			return 1
		}
		return 0
	default:
		return opsUsageError(stderr, fmt.Sprintf("unknown ops subcommand %q", args[0]))
	}
}

func parseOpsArgs(args []string) (opsArgs, error) {
	var parsed opsArgs
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			parsed.json = true
		case "--limit":
			if index+1 >= len(args) {
				return parsed, fmt.Errorf("--limit needs a number")
			}
			index++
			limit, err := strconv.Atoi(args[index])
			if err != nil || limit < 1 {
				return parsed, fmt.Errorf("--limit must be a positive number")
			}
			parsed.limit = limit
		default:
			if strings.HasPrefix(args[index], "--") {
				return parsed, fmt.Errorf("unknown flag %s", args[index])
			}
			parsed.positional = append(parsed.positional, args[index])
		}
	}
	return parsed, nil
}

func opsPathWithLimit(parsed opsArgs, segments ...string) string {
	path := opsPath(segments...)
	if parsed.limit > 0 {
		path += "?limit=" + strconv.Itoa(parsed.limit)
	}
	return path
}

func firstOr(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func opsRead(ctx context.Context, client *OpsClient, parsed opsArgs, wantArgs int, path string, human func(io.Writer, []byte) error, stdout, stderr io.Writer) int {
	if len(parsed.positional) != wantArgs {
		return opsUsageError(stderr, fmt.Sprintf("expected %d argument(s)", wantArgs))
	}
	body, err := client.Do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	if parsed.json {
		return writeRawJSON(stdout, body)
	}
	if err := human(stdout, body); err != nil {
		return writeRuntimeError(stderr, err)
	}
	return 0
}

// opsWrite sends a JSON file to Ops. Reports (spool=true) that cannot be
// delivered are kept for "den-tool ops replay" and still exit 0, so a
// reporter outage never fails the job that produced the report.
func opsWrite(ctx context.Context, client *OpsClient, parsed opsArgs, wantArgs int, method string, path func([]string) string, spool bool, stdout, stderr io.Writer) int {
	if len(parsed.positional) != wantArgs {
		return opsUsageError(stderr, fmt.Sprintf("expected %d argument(s)", wantArgs))
	}
	body, err := readJSONInput(parsed.positional[wantArgs-1])
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	target := path(parsed.positional)
	if !spool {
		response, err := client.Do(ctx, method, target, body)
		if err != nil {
			return writeRuntimeError(stderr, err)
		}
		return writeRawJSON(stdout, response)
	}
	spoolDir, err := OpsSpoolFromEnv()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	replayQuietly(ctx, client, spoolDir, stderr)
	response, err := deliverOrSpool(ctx, client, spoolDir, method, target, body, time.Now(), stderr)
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	if response == nil {
		return 0
	}
	return writeRawJSON(stdout, response)
}

func replayQuietly(ctx context.Context, client *OpsClient, spool *OpsSpool, stderr io.Writer) {
	result := spool.Replay(ctx, client)
	if result.Delivered > 0 || len(result.Rejected) > 0 {
		_, _ = fmt.Fprintf(stderr, "den-tool: replayed %d spooled ops report(s), %d rejected\n", result.Delivered, len(result.Rejected))
	}
}

func readJSONInput(name string) ([]byte, error) {
	var data []byte
	var err error
	if name == "-" {
		data, err = io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
	} else {
		data, err = os.ReadFile(name)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("%s is not valid JSON", name)
	}
	return data, nil
}

func opsUsageError(stderr io.Writer, message string) int {
	_, _ = fmt.Fprintf(stderr, "den-tool: %s\n%s\n", message, opsUsage)
	return 2
}

// The human views decode only what they print; --json returns the API body
// unchanged, so CLI and API fields are identical by construction.
type opsRunView struct {
	RunID             string     `json:"run_id"`
	State             string     `json:"state"`
	StartedAt         time.Time  `json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at"`
	ArchiveName       string     `json:"archive_name"`
	DeduplicatedBytes *int64     `json:"deduplicated_bytes"`
	DurationSeconds   *float64   `json:"duration_seconds"`
	Error             string     `json:"error"`
	Warnings          []string   `json:"warnings"`
}

type opsVerificationView struct {
	Kind      string    `json:"kind"`
	CheckType string    `json:"check_type"`
	State     string    `json:"state"`
	At        time.Time `json:"at"`
	Archive   string    `json:"archive"`
}

type opsJobView struct {
	JobID               string               `json:"job_id"`
	HostID              string               `json:"host_id"`
	Target              string               `json:"target"`
	Status              string               `json:"status"`
	StatusReason        string               `json:"status_reason"`
	NextDueAt           *time.Time           `json:"next_due_at"`
	HeartbeatAgeSeconds *int64               `json:"heartbeat_age_seconds"`
	CurrentRun          *opsRunView          `json:"current_run"`
	LastAttempt         *opsRunView          `json:"last_attempt"`
	LastSuccess         *opsRunView          `json:"last_success"`
	LastVerification    *opsVerificationView `json:"last_verification"`
	LastRestoreTest     *opsVerificationView `json:"last_restore_test"`
}

func writeOpsSummary(stdout io.Writer, body []byte) error {
	var summary struct {
		GeneratedAt time.Time `json:"generated_at"`
		Status      string    `json:"status"`
		Hosts       []struct {
			HostID   string `json:"host_id"`
			Coverage string `json:"coverage"`
			Status   string `json:"status"`
		} `json:"hosts"`
		Jobs []opsJobView `json:"jobs"`
	}
	if err := json.Unmarshal(body, &summary); err != nil {
		return fmt.Errorf("decode ops summary: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "overall: %s (at %s)\n", summary.Status, localTime(&summary.GeneratedAt))
	table := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "STATUS\tJOB\tHOST\tLAST ATTEMPT\tLAST SUCCESS\tNEXT DUE\tREASON")
	for _, job := range summary.Jobs {
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", job.Status, job.JobID, job.HostID,
			runAge(job.LastAttempt, summary.GeneratedAt), runAge(job.LastSuccess, summary.GeneratedAt),
			localTime(job.NextDueAt), job.StatusReason)
	}
	_ = table.Flush()
	for _, host := range summary.Hosts {
		if host.Coverage != "configured" {
			_, _ = fmt.Fprintf(stdout, "host %s: %s (%s)\n", host.HostID, host.Coverage, host.Status)
		}
	}
	return nil
}

func writeOpsJob(stdout io.Writer, body []byte) error {
	var detail struct {
		Job  opsJobView   `json:"job"`
		Runs []opsRunView `json:"runs"`
	}
	if err := json.Unmarshal(body, &detail); err != nil {
		return fmt.Errorf("decode ops job: %w", err)
	}
	job, now := detail.Job, time.Now()
	_, _ = fmt.Fprintf(stdout, "%s on %s -> %s\nstatus: %s (%s)\nnext due: %s\n", job.JobID, job.HostID, job.Target, job.Status, job.StatusReason, localTime(job.NextDueAt))
	if job.CurrentRun != nil && job.HeartbeatAgeSeconds != nil {
		_, _ = fmt.Fprintf(stdout, "running: %s, last heartbeat %ds ago\n", job.CurrentRun.RunID, *job.HeartbeatAgeSeconds)
	}
	_, _ = fmt.Fprintf(stdout, "last attempt: %s\nlast success: %s\n", describeRun(job.LastAttempt, now), describeRun(job.LastSuccess, now))
	_, _ = fmt.Fprintf(stdout, "last verification: %s\nlast restore test: %s\n", describeVerification(job.LastVerification), describeVerification(job.LastRestoreTest))
	for _, run := range detail.Runs {
		_, _ = fmt.Fprintf(stdout, "  %s\n", describeRun(&run, now))
	}
	return nil
}

func writeOpsErrors(stdout io.Writer, body []byte) error {
	var view struct {
		Problems []struct {
			JobID  string `json:"job_id"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"problems"`
		ProblemRuns []struct {
			JobID string `json:"job_id"`
			opsRunView
		} `json:"problem_runs"`
		FailedVerifications []struct {
			JobID          string `json:"job_id"`
			VerificationID string `json:"verification_id"`
			opsVerificationView
		} `json:"failed_verifications"`
	}
	if err := json.Unmarshal(body, &view); err != nil {
		return fmt.Errorf("decode ops errors: %w", err)
	}
	if len(view.Problems) == 0 && len(view.ProblemRuns) == 0 && len(view.FailedVerifications) == 0 {
		_, _ = fmt.Fprintln(stdout, "no job needs attention")
		return nil
	}
	for _, problem := range view.Problems {
		_, _ = fmt.Fprintf(stdout, "%s\t%s\t%s\n", problem.Status, problem.JobID, problem.Reason)
	}
	now := time.Now()
	for _, run := range view.ProblemRuns {
		_, _ = fmt.Fprintf(stdout, "recent: %s %s\n", run.JobID, describeRun(&run.opsRunView, now))
	}
	for _, check := range view.FailedVerifications {
		_, _ = fmt.Fprintf(stdout, "check: %s %s %s\n", check.JobID, check.VerificationID, describeVerification(&check.opsVerificationView))
	}
	return nil
}

func describeRun(run *opsRunView, now time.Time) string {
	if run == nil {
		return "none"
	}
	parts := []string{run.RunID, run.State, "started " + runAge(run, now)}
	if run.ArchiveName != "" {
		parts = append(parts, "archive "+run.ArchiveName)
	}
	if run.DeduplicatedBytes != nil {
		parts = append(parts, fmt.Sprintf("%d new bytes", *run.DeduplicatedBytes))
	}
	if run.DurationSeconds != nil {
		parts = append(parts, (time.Duration(*run.DurationSeconds) * time.Second).String())
	}
	if run.Error != "" {
		parts = append(parts, "error: "+firstLine(run.Error))
	}
	if len(run.Warnings) > 0 {
		parts = append(parts, fmt.Sprintf("%d warning(s): %s", len(run.Warnings), firstLine(run.Warnings[0])))
	}
	return strings.Join(parts, ", ")
}

func describeVerification(verification *opsVerificationView) string {
	if verification == nil {
		return "none recorded"
	}
	name := verification.Kind
	if verification.CheckType != "" {
		name = verification.CheckType
	}
	return fmt.Sprintf("%s %s at %s (archive %s)", name, verification.State, localTime(&verification.At), verification.Archive)
}

func runAge(run *opsRunView, now time.Time) string {
	if run == nil {
		return "never"
	}
	return fmt.Sprintf("%s ago", now.Sub(run.StartedAt).Round(time.Minute))
}

func localTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04 MST")
}

func firstLine(value string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(value), "\n")
	if len(line) > 160 {
		line = line[:160] + "..."
	}
	return line
}
