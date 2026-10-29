package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// SnapRAID is storage maintenance, not a backup: a parity sync lets a failed
// data disk be rebuilt, and the weekly scrub checks part of the array. Each
// systemd invocation of a SnapRAID unit becomes one maintenance run, read from
// the NAS journal; the scrub is also recorded as a verification.

const (
	journalStartMessage     = "7d4958e842da4a758f6c1cdc7b36dcc5"
	snapraidScanMarker      = "snapraid-scan "
	snapraidRecentLookback  = 72 * time.Hour
	snapraidScanFreshWindow = 24 * time.Hour
	snapraidErrorTailLines  = 15
)

var (
	snapraidErrorCount = regexp.MustCompile(`^\s*(\d+) (file|io|data) errors`)
	snapraidProgress   = regexp.MustCompile(`^\s*(\w+\s+)?\d+%|^\s*\|_+|wait time`)
)

type journalEntry struct {
	Realtime            string          `json:"__REALTIME_TIMESTAMP"`
	Message             json.RawMessage `json:"MESSAGE"`
	MessageID           string          `json:"MESSAGE_ID"`
	InvocationID        string          `json:"INVOCATION_ID"`
	SystemdInvocationID string          `json:"_SYSTEMD_INVOCATION_ID"`
	JobResult           string          `json:"JOB_RESULT"`
	ExitStatus          string          `json:"EXIT_STATUS"`
	Unit                string          `json:"UNIT"`
	SystemdUnit         string          `json:"_SYSTEMD_UNIT"`
}

func (e journalEntry) invocation() string {
	return firstNonEmpty(e.InvocationID, e.SystemdInvocationID)
}

func (e journalEntry) unit() string {
	return firstNonEmpty(e.Unit, e.SystemdUnit)
}

func (e journalEntry) at() time.Time {
	micros, err := strconv.ParseInt(e.Realtime, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMicro(micros).UTC()
}

// text decodes MESSAGE, which journald sends as an array of bytes when it is
// not valid UTF-8.
func (e journalEntry) text() string {
	var text string
	if json.Unmarshal(e.Message, &text) == nil {
		return text
	}
	var raw []byte
	var numbers []int
	if json.Unmarshal(e.Message, &numbers) == nil {
		for _, number := range numbers {
			raw = append(raw, byte(number))
		}
		return strings.ToValidUTF8(string(raw), "?")
	}
	return ""
}

type unitState struct {
	ActiveState  string
	InvocationID string
}

type snapraidInvocation struct {
	id, unit                         string
	startedAt, updatedAt, finishedAt time.Time
	hasStart                         bool
	jobResult, exitStatus            string
	phase                            string
	lines                            []string
	errorCounts                      []string
	scrubStarted, scrubFinished      time.Time
}

func collectSnapraid(ctx context.Context, source opsCollectSource, env opsCollectEnv) ([]opsCollectWrite, error) {
	since := env.lookback
	if scan, ok := env.cache.Entries["done "+snapraidScanMarker+source.JobID]; ok && env.now.Sub(scan.At) < snapraidScanFreshWindow {
		since = min(since, snapraidRecentLookback)
	}
	output, err := env.remote(ctx, source.SSH, snapraidCommand(source.Units, since))
	if err != nil {
		return nil, err
	}
	states, entries, err := parseSnapraidOutput(output)
	if err != nil {
		return nil, err
	}
	writes := snapraidWrites(source, states, entries, env.now)
	env.cache.markDone(snapraidScanMarker+source.JobID, env.now)
	return writes, nil
}

// snapraidCommand reads the units' state and journal in one SSH call. The
// per-file "WARNING! Ignoring special file" and "Data change" lines are
// dropped on the NAS: they are routine and make up most of the journal.
func snapraidCommand(units []string, since time.Duration) string {
	quoted := make([]string, 0, len(units))
	selectors := make([]string, 0, len(units))
	for _, unit := range units {
		quoted = append(quoted, shellQuote(unit))
		selectors = append(selectors, "-u "+shellQuote(unit))
	}
	return fmt.Sprintf("systemctl show -p Id,ActiveState,InvocationID %s; echo ---journal---; "+
		"journalctl %s --since=-%ds -o json --output-fields=MESSAGE,MESSAGE_ID,INVOCATION_ID,_SYSTEMD_INVOCATION_ID,JOB_RESULT,EXIT_STATUS,UNIT,_SYSTEMD_UNIT "+
		"| grep -v -e '\"MESSAGE\":\"WARNING! Ignoring special' -e '\"MESSAGE\":\"Data change at file' || true",
		strings.Join(quoted, " "), strings.Join(selectors, " "), int(since.Seconds()))
}

func parseSnapraidOutput(output []byte) (map[string]unitState, []journalEntry, error) {
	head, journal, found := bytes.Cut(output, []byte("---journal---\n"))
	if !found {
		return nil, nil, fmt.Errorf("unexpected snapraid output: no journal marker")
	}
	states := map[string]unitState{}
	for _, block := range strings.Split(strings.TrimSpace(string(head)), "\n\n") {
		fields := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			key, value, _ := strings.Cut(line, "=")
			fields[key] = value
		}
		if fields["Id"] != "" {
			states[fields["Id"]] = unitState{ActiveState: fields["ActiveState"], InvocationID: fields["InvocationID"]}
		}
	}
	var entries []journalEntry
	scanner := bufio.NewScanner(bytes.NewReader(journal))
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry journalEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, nil, fmt.Errorf("parse journal entry: %w", err)
		}
		entries = append(entries, entry)
	}
	return states, entries, scanner.Err()
}

func snapraidWrites(source opsCollectSource, states map[string]unitState, entries []journalEntry, now time.Time) []opsCollectWrite {
	invocations := groupSnapraidInvocations(entries)
	var writes []opsCollectWrite
	for _, invocation := range invocations {
		if !invocation.hasStart {
			// It started before the journal window; its start time is unknown.
			continue
		}
		state := states[invocation.unit]
		running := invocation.jobResult == "" && state.InvocationID == invocation.id &&
			(state.ActiveState == "active" || state.ActiveState == "activating" || state.ActiveState == "reloading")
		if invocation.jobResult == "" && !running {
			continue
		}
		mode := strings.TrimSuffix(strings.TrimPrefix(invocation.unit, "snapraid-"), ".service")
		runID := strings.TrimSuffix(invocation.unit, ".service") + "-" + invocation.id
		report := map[string]any{
			"job_id": source.JobID, "run_id": runID, "host": source.HostID, "engine": "snapraid", "runner": "systemd",
			"kind": "maintenance", "phase": invocation.phase, "started_at": invocation.startedAt,
			"scope":    map[bool]string{true: "parity sync and 25% scrub", false: "parity sync"}[mode == "weekly"],
			"log_path": "/var/log/snapraid-maintenance.log", "unit": invocation.unit, "warnings": []string{},
		}
		if len(invocation.errorCounts) > 0 {
			report["source_notices"] = invocation.errorCounts
		}
		if running {
			// The unit is active right now, so this observation is the heartbeat.
			report["state"] = "running"
			report["updated_at"] = now
		} else {
			report["updated_at"] = invocation.finishedAt
			report["finished_at"] = invocation.finishedAt
			if code, err := strconv.Atoi(invocation.exitStatus); err == nil {
				report["exit_code"] = code
			} else if invocation.jobResult == "done" {
				// systemd logs an exit status only when the service fails.
				report["exit_code"] = 0
			}
			if invocation.jobResult == "done" {
				report["state"] = "success"
			} else {
				report["state"] = "failed"
				report["error"] = fmt.Sprintf("%s finished with result %q:\n%s", invocation.unit, invocation.jobResult,
					strings.Join(lastLines(invocation.lines, snapraidErrorTailLines), "\n"))
			}
		}
		writes = append(writes, opsCollectWrite{Path: opsPath("jobs", source.JobID, "runs", runID), Body: mustJSON(report)})
		if check := snapraidScrubCheck(invocation, runID); check != nil {
			writes = append(writes, opsCollectWrite{Path: opsPath("jobs", source.JobID, "verifications", runID+":scrub"), Body: mustJSON(check)})
		}
	}
	return writes
}

func groupSnapraidInvocations(entries []journalEntry) []*snapraidInvocation {
	byID := map[string]*snapraidInvocation{}
	var order []*snapraidInvocation
	for _, entry := range entries {
		id := entry.invocation()
		if id == "" {
			continue
		}
		invocation, ok := byID[id]
		if !ok {
			invocation = &snapraidInvocation{id: id, unit: entry.unit(), startedAt: entry.at(), phase: "starting"}
			byID[id] = invocation
			order = append(order, invocation)
		}
		at := entry.at()
		invocation.updatedAt = at
		if entry.MessageID == journalStartMessage {
			invocation.hasStart = true
			invocation.startedAt = at
		}
		if entry.JobResult != "" {
			invocation.jobResult = entry.JobResult
			invocation.finishedAt = at
		}
		if entry.ExitStatus != "" {
			invocation.exitStatus = entry.ExitStatus
		}
		text := entry.text()
		switch {
		case strings.Contains(text, "Waiting for the maintenance lock"):
			invocation.phase = "waiting for maintenance lock"
		case strings.Contains(text, "Starting daily sync"), strings.Contains(text, "Starting weekly sync"):
			invocation.phase = "sync"
		case strings.Contains(text, "Starting scrub"):
			invocation.phase = "scrub"
			invocation.scrubStarted = at
		case strings.Contains(text, "Scrub completed successfully"):
			invocation.scrubFinished = at
		}
		if match := snapraidErrorCount.FindStringSubmatch(text); match != nil && match[1] != "0" {
			invocation.errorCounts = append(invocation.errorCounts, strings.TrimSpace(text))
		}
		if entry.SystemdInvocationID != "" && text != "" && !snapraidProgress.MatchString(text) {
			invocation.lines = append(invocation.lines, text)
		}
	}
	return order
}

func snapraidScrubCheck(invocation *snapraidInvocation, runID string) map[string]any {
	if invocation.scrubStarted.IsZero() {
		return nil
	}
	check := map[string]any{
		"kind": "snapraid-scrub", "run_id": runID, "full_data_read": false,
		"scope": "SnapRAID scrub: 25% of the array, blocks not scrubbed in the last 10 days",
	}
	switch {
	case !invocation.scrubFinished.IsZero():
		check["state"], check["at"] = "success", invocation.scrubFinished
	case invocation.jobResult != "" && invocation.jobResult != "done":
		check["state"], check["at"] = "failed", invocation.finishedAt
		check["detail"] = strings.Join(lastLines(invocation.lines, snapraidErrorTailLines), "\n")
	default:
		return nil
	}
	return check
}

func lastLines(lines []string, count int) []string {
	return slices.Clone(lines[max(0, len(lines)-count):])
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
