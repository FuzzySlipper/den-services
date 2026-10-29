package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"
)

const resourcesUsage = `usage: den-tool resources [--json] [--url URL] <command> [flags]

Inspect the workshop machines, share installations and clock work in/out.
Claims are advisory: they warn about overlaps and never grant exclusivity.

  status | hosts                 machine activity, native queues, current work
  resources [ID]                shared installs, paths and lifecycle records
  work                          the work ledger
  register FILE                 register/update a resource from a JSON file
  touch ID [--owner O] [--note N]
                                record actual use of a resource today
  maintain ID --lifecycle L --note N [--owner O]
                                confirm lifecycle (maintained, experimental,
                                retired, unknown) and update its notes
  check-in --purpose P [work flags]
  run --purpose P [work flags] -- COMMAND [ARGS...]
                                check in, heartbeat while COMMAND runs, then
                                record completed/failed/cancelled
  report --source S --job-id J --purpose P [work flags] [--state S]
                                an application's report for its own job
  heartbeat ID [--note N]       renew a check-in
  check-out ID [--state completed|failed|cancelled] [--note N]
  report-host HOST FILE         push a telemetry snapshot (probe schema)
  collect [--config FILE] [--once] [--dry-run]
                                the den-agents collector (see docs/resources-operations.md)

work flags: --host H (default this machine's hostname) --owner O --project P
  (default the current directory) --resource ID (repeatable) --ttl SECONDS
  (default 180) --note N --state queued|running

DEN_RESOURCES_URL or --url selects the service (default ` + defaultResourcesURL + `).
DEN_RESOURCES_OWNER (or ASSET_RESOURCES_OWNER) sets the default owner.`

// resourcesFlags is a small interleaving parser: the old asset-resources CLI
// accepted flags before and after positional arguments, and scripts rely on it.
type resourcesFlags struct {
	values     map[string][]string
	positional []string
	command    []string
}

func parseResourcesFlags(args []string, valued map[string]bool, booleans map[string]bool) (resourcesFlags, error) {
	parsed := resourcesFlags{values: map[string][]string{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			parsed.command = args[i+1:]
			break
		}
		if !strings.HasPrefix(arg, "--") || arg == "-" {
			parsed.positional = append(parsed.positional, arg)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		switch {
		case booleans[name]:
			if hasValue {
				return parsed, fmt.Errorf("--%s takes no value", name)
			}
			parsed.values[name] = append(parsed.values[name], "true")
		case valued[name]:
			if !hasValue {
				if i+1 >= len(args) {
					return parsed, fmt.Errorf("--%s requires a value", name)
				}
				i++
				value = args[i]
			}
			parsed.values[name] = append(parsed.values[name], value)
		default:
			return parsed, fmt.Errorf("unknown flag --%s", name)
		}
	}
	return parsed, nil
}

func (f resourcesFlags) get(name string, fallback string) string {
	if values := f.values[name]; len(values) > 0 {
		return values[len(values)-1]
	}
	return fallback
}

func (f resourcesFlags) has(name string) bool { return len(f.values[name]) > 0 }

func handleResources(args []string, stdout, stderr io.Writer) int {
	// Global flags may come before or after the subcommand, but never from
	// the wrapped command after "--".
	split := len(args)
	for i, arg := range args {
		if arg == "--" {
			split = i
			break
		}
	}
	jsonOutput, urlFlag := false, ""
	var rest []string
	for i := 0; i < split; i++ {
		switch {
		case args[i] == "--json":
			jsonOutput = true
		case args[i] == "--url" && i+1 < split:
			urlFlag = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--url="):
			urlFlag = strings.TrimPrefix(args[i], "--url=")
		default:
			rest = append(rest, args[i])
		}
	}
	rest = append(rest, args[split:]...)
	if len(rest) == 0 || rest[0] == "--help" || rest[0] == "-h" || rest[0] == "help" {
		_, _ = fmt.Fprintln(stderr, resourcesUsage)
		if len(rest) == 0 {
			return 2
		}
		return 0
	}
	client, err := NewResourcesClient(resourcesURL(urlFlag))
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	command, rest := rest[0], rest[1:]
	if command == "collect" {
		return runResourcesCollect(rest, client, stdout, stderr)
	}
	if command == "run" {
		return runResourcesRun(rest, client, stderr)
	}
	result, err := resourcesCommand(context.Background(), client, command, rest, jsonOutput, stdout)
	if err != nil {
		var usage *usageError
		if errors.As(err, &usage) {
			_, _ = fmt.Fprintf(stderr, "den-tool resources: %s\n\n%s\n", usage.message, resourcesUsage)
			return 2
		}
		_, _ = fmt.Fprintf(stderr, "den-tool resources: %v\n", err)
		return 1
	}
	if result != nil {
		_, _ = fmt.Fprintln(stdout, indentJSON(result))
	}
	return 0
}

type usageError struct{ message string }

func (e *usageError) Error() string { return e.message }

func usagef(format string, args ...any) error {
	return &usageError{message: fmt.Sprintf(format, args...)}
}

var workFlagNames = map[string]bool{ //nolint:gochecknoglobals
	"host": true, "purpose": true, "owner": true, "project": true, "resource": true, "ttl": true, "note": true, "state": true,
}

// resourcesCommand runs one non-streaming subcommand. It returns the JSON to
// print, or nil when it already wrote a human-readable listing.
func resourcesCommand(ctx context.Context, client *ResourcesClient, command string, args []string, jsonOutput bool, stdout io.Writer) ([]byte, error) {
	switch command {
	case "status", "hosts", "work":
		if len(args) != 0 {
			return nil, usagef("%s takes no arguments", command)
		}
		body, err := client.Do(ctx, http.MethodGet, resourcesPath("state"), nil)
		if err != nil || jsonOutput {
			return body, err
		}
		return nil, printResourcesState(stdout, body, command)
	case "resources":
		if len(args) > 1 {
			return nil, usagef("resources takes at most one ID")
		}
		if len(args) == 1 {
			return client.Do(ctx, http.MethodGet, resourcesPath("directory", args[0]), nil)
		}
		body, err := client.Do(ctx, http.MethodGet, resourcesPath("state"), nil)
		if err != nil || jsonOutput {
			return body, err
		}
		return nil, printResourcesState(stdout, body, command)
	case "register":
		if len(args) != 1 {
			return nil, usagef("register takes one JSON file")
		}
		body, err := readJSONArgument(args[0])
		if err != nil {
			return nil, err
		}
		return client.Do(ctx, http.MethodPost, resourcesPath("directory"), body)
	case "touch", "maintain":
		flags, err := parseResourcesFlags(args, map[string]bool{"owner": true, "note": true, "lifecycle": true}, nil)
		if err != nil {
			return nil, usagef("%v", err)
		}
		if len(flags.positional) != 1 {
			return nil, usagef("%s takes one resource ID", command)
		}
		update := map[string]any{"id": flags.positional[0], "owner": flags.get("owner", currentUser())}
		if command == "touch" {
			update["touch"] = true
		} else {
			if !flags.has("lifecycle") || !flags.has("note") {
				return nil, usagef("maintain requires --lifecycle and --note")
			}
			update["confirm"], update["lifecycle"] = true, flags.get("lifecycle", "")
		}
		if note := flags.get("note", ""); note != "" {
			update["notes"] = note
		}
		return postJSON(ctx, client, resourcesPath("directory"), update)
	case "check-in", "report":
		valued := map[string]bool{"source": true, "job-id": true}
		for name := range workFlagNames {
			valued[name] = true
		}
		flags, err := parseResourcesFlags(args, valued, nil)
		if err != nil {
			return nil, usagef("%v", err)
		}
		if len(flags.positional) != 0 {
			return nil, usagef("%s takes only flags", command)
		}
		report, err := workReport(flags, command == "report")
		if err != nil {
			return nil, err
		}
		if command == "report" {
			if !flags.has("source") || !flags.has("job-id") {
				return nil, usagef("report requires --source and --job-id")
			}
			report["source"], report["external_id"] = flags.get("source", ""), flags.get("job-id", "")
		}
		return postJSON(ctx, client, resourcesPath("work"), report)
	case "heartbeat", "check-out":
		flags, err := parseResourcesFlags(args, map[string]bool{"note": true, "state": true}, nil)
		if err != nil {
			return nil, usagef("%v", err)
		}
		if len(flags.positional) != 1 {
			return nil, usagef("%s takes one check-in ID", command)
		}
		update := map[string]any{}
		if flags.has("note") {
			update["note"] = flags.get("note", "")
		}
		if command == "check-out" {
			state := flags.get("state", "completed")
			if state != "completed" && state != "failed" && state != "cancelled" {
				return nil, usagef("--state must be completed, failed or cancelled")
			}
			update["state"] = state
		} else if flags.has("state") {
			return nil, usagef("heartbeat takes no --state; use check-out")
		}
		return postJSON(ctx, client, resourcesPath("work", flags.positional[0]), update)
	case "report-host":
		if len(args) != 2 {
			return nil, usagef("report-host takes HOST and a telemetry JSON file")
		}
		snapshot, err := readJSONArgument(args[1])
		if err != nil {
			return nil, err
		}
		return postJSON(ctx, client, resourcesPath("hosts", args[0], "telemetry"), map[string]any{"telemetry": json.RawMessage(snapshot)})
	default:
		return nil, usagef("unknown command %q", command)
	}
}

// workReport builds a check-in from the work flags. Reports may carry any
// state; a check-in starts queued or running.
func workReport(flags resourcesFlags, anyState bool) (map[string]any, error) {
	if !flags.has("purpose") {
		return nil, usagef("--purpose is required: say what the work is")
	}
	ttl, err := strconv.ParseFloat(flags.get("ttl", "180"), 64)
	if err != nil || ttl <= 0 {
		return nil, usagef("--ttl must be a positive number of seconds")
	}
	state := flags.get("state", "running")
	allowed := map[string]bool{"queued": true, "running": true}
	if anyState {
		allowed["completed"], allowed["failed"], allowed["cancelled"] = true, true, true
	}
	if !allowed[state] {
		return nil, usagef("--state %q is not allowed here", state)
	}
	host, _ := os.Hostname()
	project, _ := os.Getwd()
	resources := flags.values["resource"]
	if resources == nil {
		resources = []string{}
	}
	return map[string]any{
		"host": flags.get("host", host), "title": flags.get("purpose", ""), "owner": flags.get("owner", defaultOwner()),
		"project": flags.get("project", project), "resources": resources, "ttl_seconds": ttl, "state": state,
		"source": "check-in", "note": flags.get("note", ""),
	}, nil
}

func defaultOwner() string {
	for _, name := range []string{"DEN_RESOURCES_OWNER", "ASSET_RESOURCES_OWNER"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return currentUser()
}

func currentUser() string {
	if current, err := user.Current(); err == nil && current.Username != "" {
		return current.Username
	}
	return "unknown"
}

func postJSON(ctx context.Context, client *ResourcesClient, path string, value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return client.Do(ctx, http.MethodPost, path, body)
}

func readJSONArgument(path string) ([]byte, error) {
	var body []byte
	var err error
	if path == "-" {
		body, err = io.ReadAll(os.Stdin)
	} else {
		body, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("%s is not valid JSON", path)
	}
	return body, nil
}

func indentJSON(body []byte) string {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return string(body)
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return string(body)
	}
	return string(encoded)
}

// resourcesState is the part of GET /v1/resources/state the listings print.
type resourcesState struct {
	GeneratedAt time.Time `json:"generated_at"`
	Hosts       []struct {
		ID        string     `json:"id"`
		Status    string     `json:"status"`
		Error     *string    `json:"error"`
		SampledAt *time.Time `json:"sampled_at"`
		Telemetry *struct {
			CPUPercent *float64 `json:"cpu_percent"`
			Memory     struct {
				AvailableBytes *float64 `json:"available_bytes"`
			} `json:"memory"`
			GPUs []struct {
				Name        string   `json:"name"`
				Utilization *float64 `json:"utilization_percent"`
				MemoryUsed  *float64 `json:"memory_used_bytes"`
				MemoryTotal *float64 `json:"memory_total_bytes"`
				MemoryNote  string   `json:"memory_note"`
				GTTUsed     *float64 `json:"gtt_used_bytes"`
			} `json:"gpus"`
		} `json:"telemetry"`
		Apps []struct {
			Name      string    `json:"name"`
			Online    bool      `json:"online"`
			Running   *int      `json:"running"`
			Queued    *int      `json:"queued"`
			Stale     bool      `json:"stale"`
			CheckedAt time.Time `json:"checked_at"`
		} `json:"apps"`
	} `json:"hosts"`
	Resources []struct {
		ID                 string     `json:"id"`
		Host               string     `json:"host"`
		Lifecycle          string     `json:"lifecycle"`
		Path               string     `json:"path"`
		Sharing            string     `json:"sharing"`
		LastUsedAt         *time.Time `json:"last_used_at"`
		ObservedActivityAt *time.Time `json:"observed_activity_at"`
		ConfirmedAt        *time.Time `json:"confirmed_at"`
	} `json:"resources"`
	Work []struct {
		ID    string `json:"id"`
		Host  string `json:"host"`
		State string `json:"state"`
		Title string `json:"title"`
		Owner string `json:"owner"`
		Stale bool   `json:"stale"`
	} `json:"work"`
}

func printResourcesState(w io.Writer, body []byte, mode string) error {
	var state resourcesState
	if err := json.Unmarshal(body, &state); err != nil {
		return fmt.Errorf("decode resources state: %w", err)
	}
	switch mode {
	case "status", "hosts":
		for _, host := range state.Hosts {
			cpu, available, age := "?", "unknown", "never"
			if t := host.Telemetry; t != nil {
				if t.CPUPercent != nil {
					cpu = strconv.FormatFloat(*t.CPUPercent, 'f', -1, 64)
				}
				available = gib(t.Memory.AvailableBytes)
			}
			if host.SampledAt != nil {
				age = fmt.Sprintf("%ds ago", int(state.GeneratedAt.Sub(*host.SampledAt).Seconds()))
			}
			_, _ = fmt.Fprintf(w, "%-14s %-8s RAM available %-14s CPU %s%%   sample %s\n", host.ID, host.Status, available, cpu, age)
			if host.Error != nil {
				_, _ = fmt.Fprintf(w, "  telemetry: %s\n", *host.Error)
			}
			if t := host.Telemetry; t != nil {
				for _, gpu := range t.GPUs {
					_, _ = fmt.Fprintf(w, "  %s: %s%% GPU; %s / %s; %s\n", gpu.Name, number(gpu.Utilization), gib(gpu.MemoryUsed), gib(gpu.MemoryTotal), gpu.MemoryNote)
					if gpu.GTTUsed != nil {
						_, _ = fmt.Fprintf(w, "    GTT %s; uses system RAM\n", gib(gpu.GTTUsed))
					}
				}
			}
			for _, app := range host.Apps {
				line := "unreachable"
				switch {
				case app.Online && app.Running != nil && app.Queued != nil:
					line = fmt.Sprintf("running %d, queued %d", *app.Running, *app.Queued)
				case app.Online:
					line = "healthy; queue unknown"
				}
				stale := ""
				if app.Stale {
					stale = "STALE "
				}
				_, _ = fmt.Fprintf(w, "  %s: %s%s (checked %s)\n", app.Name, stale, line, recordedTime(&app.CheckedAt))
			}
			for _, work := range state.Work {
				if work.Host == host.ID && (work.State == "queued" || work.State == "running") {
					_, _ = fmt.Fprintf(w, "  %s%s: %s — %s [%s]\n", staleLabel(work.Stale), work.State, work.Title, work.Owner, work.ID)
				}
			}
		}
		_, _ = fmt.Fprintln(w, "\nQuiet means no heavy activity measured in this sample. Claims are advisory; inspect before maintenance.")
	case "resources":
		for _, r := range state.Resources {
			_, _ = fmt.Fprintf(w, "%-23s %-12s %-13s %s\n", r.ID, r.Host, r.Lifecycle, r.Path)
			_, _ = fmt.Fprintf(w, "  %s\n", r.Sharing)
			_, _ = fmt.Fprintf(w, "  use: %s; observed: %s; confirmed: %s\n", recordedTime(r.LastUsedAt), recordedTime(r.ObservedActivityAt), recordedTime(r.ConfirmedAt))
		}
	default:
		for _, work := range state.Work {
			_, _ = fmt.Fprintf(w, "%s  %s%-10s %-12s %s (%s)\n", work.ID, staleLabel(work.Stale), work.State, work.Host, work.Title, work.Owner)
		}
	}
	return nil
}

func gib(value *float64) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprintf("%.1f GiB", *value/(1<<30))
}

func number(value *float64) string {
	if value == nil {
		return "?"
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}

func recordedTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "not recorded"
	}
	return value.Local().Format("2006-01-02T15:04-07:00")
}

func staleLabel(stale bool) string {
	if stale {
		return "STALE "
	}
	return ""
}
