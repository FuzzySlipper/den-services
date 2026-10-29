package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

// "den-tool resources collect" is the Resources service's collector. It runs
// on den-agents, which has SSH access to every workshop host, and pushes what
// it reads to the service. Everything here is read-only: it never starts or
// stops inference, kills jobs, unloads models or moves files.

//go:embed resources_probe/probe.py resources_probe/probe-windows.ps1
var resourcesProbes embed.FS

const (
	defaultCollectPoll   = 20 * time.Second
	defaultProbeTimeout  = 28 * time.Second
	appProbeTimeout      = 3 * time.Second
	workbenchTTLSeconds  = 90
	collectErrorTailSize = 600
)

type resourcesCollectConfig struct {
	PollSeconds        int                               `json:"poll_seconds"`
	Hosts              []resourcesCollectHost            `json:"hosts"`
	WorkbenchRoots     []string                          `json:"workbench_roots"`
	WorkbenchResources map[string]resourcesWorkbenchLink `json:"workbench_resources"`
}

type resourcesCollectHost struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Access string `json:"access"`
	Note   string `json:"note"`
	// Transport is local, ssh, windows or push (an external reporter owns
	// the machine snapshot; apps are still probed).
	Transport     string                `json:"transport"`
	SSH           []string              `json:"ssh"`
	ProbeScript   string                `json:"probe_script"`
	Disks         []string              `json:"disks"`
	DockerSockets []string              `json:"docker_sockets"`
	Apps          []resourcesCollectApp `json:"apps"`
	TimeoutSecs   int                   `json:"timeout"`
}

type resourcesCollectApp struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind"` // comfy (native queue) or health
	URL        string   `json:"url"`
	HealthPath string   `json:"health_path"`
	Resources  []string `json:"resources"`
}

type resourcesWorkbenchLink struct {
	Host      string   `json:"host"`
	Resources []string `json:"resources"`
}

// collectWrite is one request the collector would send; --dry-run prints them.
type collectWrite struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
}

type resourcesCollector struct {
	config   resourcesCollectConfig
	client   *ResourcesClient
	dryRun   bool
	stdout   io.Writer
	stderr   io.Writer
	runProbe func(ctx context.Context, argv []string, stdin []byte) ([]byte, error)
	httpGet  func(ctx context.Context, url string) ([]byte, error)
	mu       sync.Mutex
	writes   []collectWrite
}

func runResourcesCollect(args []string, client *ResourcesClient, stdout, stderr io.Writer) int {
	flags, err := parseResourcesFlags(args, map[string]bool{"config": true}, map[string]bool{"once": true, "dry-run": true})
	if err != nil || len(flags.positional) != 0 {
		_, _ = fmt.Fprintf(stderr, "den-tool resources collect: usage: collect [--config FILE] [--once] [--dry-run] (%v)\n", err)
		return 2
	}
	path := flags.get("config", os.Getenv("DEN_RESOURCES_COLLECT_CONFIG"))
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".config", "den-resources-collect", "config.json")
	}
	config, err := loadResourcesCollectConfig(path)
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	collector := &resourcesCollector{
		config: config, client: client, dryRun: flags.has("dry-run"), stdout: stdout, stderr: stderr,
		runProbe: runProbeCommand, httpGet: httpGetBody,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if flags.has("once") || collector.dryRun {
		collector.round(ctx)
		if collector.dryRun {
			writeJSON(stdout, collector.writes)
		}
		return 0
	}
	poll := time.Duration(config.PollSeconds) * time.Second
	for {
		started := time.Now()
		collector.round(ctx)
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(max(time.Second, poll-time.Since(started))):
		}
	}
}

func loadResourcesCollectConfig(path string) (resourcesCollectConfig, error) {
	var config resourcesCollectConfig
	body, err := os.ReadFile(path)
	if err != nil {
		return config, fmt.Errorf("read collect config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("parse collect config %s: %w", path, err)
	}
	if config.PollSeconds <= 0 {
		config.PollSeconds = int(defaultCollectPoll.Seconds())
	}
	for _, host := range config.Hosts {
		switch host.Transport {
		case "local", "push":
		case "ssh", "windows":
			if len(host.SSH) == 0 {
				return config, fmt.Errorf("host %s: %s transport needs ssh arguments", host.ID, host.Transport)
			}
			if host.Transport == "windows" && host.ProbeScript == "" {
				return config, fmt.Errorf("host %s: windows transport needs probe_script", host.ID)
			}
		default:
			return config, fmt.Errorf("host %s: transport must be local, ssh, windows or push", host.ID)
		}
	}
	return config, nil
}

// round registers the hosts, probes every host and app in parallel and
// mirrors the Workbench queue. A failure on one host never stops the others.
func (c *resourcesCollector) round(ctx context.Context) {
	c.send(ctx, http.MethodPut, resourcesPath("settings"), map[string]any{"poll_seconds": c.config.PollSeconds})
	for position, host := range c.config.Hosts {
		c.send(ctx, http.MethodPut, resourcesPath("hosts", host.ID), map[string]any{
			"name": firstNonEmptyString(host.Name, host.ID), "role": host.Role, "access": host.Access,
			"note": host.Note, "position": position,
		})
	}
	paths := c.resourcePaths(ctx)
	var group sync.WaitGroup
	for _, host := range c.config.Hosts {
		group.Add(2)
		go func() { defer group.Done(); c.machine(ctx, host, paths[host.ID]) }()
		go func() { defer group.Done(); c.apps(ctx, host) }()
	}
	group.Add(1)
	go func() { defer group.Done(); c.workbench(ctx) }()
	group.Wait()
}

// resourcePaths asks the directory which paths each host's probe checks.
func (c *resourcesCollector) resourcePaths(ctx context.Context) map[string][]string {
	paths := map[string][]string{}
	body, err := c.client.Do(ctx, http.MethodGet, resourcesPath("directory"), nil)
	if err != nil {
		c.warn("reading the resource directory: %v", err)
		return paths
	}
	var directory struct {
		Resources []struct {
			Host      string `json:"host"`
			Path      string `json:"path"`
			ProbePath string `json:"probe_path"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(body, &directory); err != nil {
		c.warn("decoding the resource directory: %v", err)
		return paths
	}
	for _, r := range directory.Resources {
		path := firstNonEmptyString(r.ProbePath, r.Path)
		if !slices.Contains(paths[r.Host], path) {
			paths[r.Host] = append(paths[r.Host], path)
		}
	}
	return paths
}

func (c *resourcesCollector) machine(ctx context.Context, host resourcesCollectHost, paths []string) {
	if host.Transport == "push" {
		return
	}
	disks := host.Disks
	if disks == nil {
		disks = []string{"/", "/home"}
	}
	options, _ := json.Marshal(map[string]any{
		"paths": nonNilStrings(paths), "disks": disks, "docker_sockets": nonNilStrings(host.DockerSockets),
	})
	argv, stdin, err := probeCommand(host, options)
	if err != nil {
		c.send(ctx, http.MethodPost, resourcesPath("hosts", host.ID, "telemetry"), map[string]any{"error": err.Error()})
		return
	}
	timeout := defaultProbeTimeout
	if host.TimeoutSecs > 0 {
		timeout = time.Duration(host.TimeoutSecs) * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := c.runProbe(probeCtx, argv, stdin)
	output = bytes.TrimPrefix(bytes.TrimSpace(output), []byte("\xef\xbb\xbf"))
	if err == nil && !json.Valid(output) {
		err = fmt.Errorf("probe output is not JSON: %s", tail(string(output), 200))
	}
	if err != nil {
		c.send(ctx, http.MethodPost, resourcesPath("hosts", host.ID, "telemetry"), map[string]any{"error": err.Error()})
		return
	}
	c.send(ctx, http.MethodPost, resourcesPath("hosts", host.ID, "telemetry"), map[string]any{"telemetry": json.RawMessage(output)})
}

// probeCommand builds the read-only probe: the Linux probe goes over SSH
// stdin (only python3 is needed on the host); Windows runs a copied script.
func probeCommand(host resourcesCollectHost, options []byte) ([]string, []byte, error) {
	if host.Transport == "windows" {
		encoded := base64.StdEncoding.EncodeToString(options)
		command := fmt.Sprintf("powershell.exe -NoProfile -NonInteractive -File %s -OptionsBase64 %s", host.ProbeScript, encoded)
		return append(sshArgv(host.SSH), command), nil, nil
	}
	script, err := resourcesProbes.ReadFile("resources_probe/probe.py")
	if err != nil {
		return nil, nil, err
	}
	if host.Transport == "local" {
		return []string{"python3", "-", string(options)}, script, nil
	}
	return append(sshArgv(host.SSH), "python3 - "+shellQuote(string(options))), script, nil
}

func sshArgv(args []string) []string {
	argv := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=4", "-o", "ConnectionAttempts=1"}
	home, _ := os.UserHomeDir()
	for _, arg := range args {
		if strings.HasPrefix(arg, "~/") {
			arg = filepath.Join(home, arg[2:])
		}
		argv = append(argv, arg)
	}
	return argv
}

func runProbeCommand(ctx context.Context, argv []string, stdin []byte) ([]byte, error) {
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("probe timed out")
		}
		message := strings.TrimSpace(firstNonEmptyString(stderr.String(), stdout.String(), err.Error()))
		return nil, errors.New(tail(message, collectErrorTailSize))
	}
	return stdout.Bytes(), nil
}

func httpGetBody(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, appProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return body, nil
}

// apps probes native queues (ComfyUI) and HTTP health. Health establishes
// readiness only; its queue stays unknown.
func (c *resourcesCollector) apps(ctx context.Context, host resourcesCollectHost) {
	if len(host.Apps) == 0 {
		return
	}
	rows := make([]map[string]any, 0, len(host.Apps))
	for _, app := range host.Apps {
		row := map[string]any{
			"name": app.Name, "kind": app.Kind, "url": app.URL, "resources": nonNilStrings(app.Resources),
			"online": false, "running": nil, "queued": nil, "jobs": []any{}, "error": nil,
		}
		base := strings.TrimRight(app.URL, "/")
		if app.Kind == "comfy" {
			body, err := c.httpGet(ctx, base+"/queue")
			if err == nil {
				err = comfyQueue(body, row)
			}
			if err != nil {
				row["error"] = tail(err.Error(), 200)
			}
		} else {
			if _, err := c.httpGet(ctx, base+firstNonEmptyString(app.HealthPath, "/health")); err != nil {
				row["error"] = tail(err.Error(), 200)
			} else {
				row["online"] = true
			}
		}
		rows = append(rows, row)
	}
	c.send(ctx, http.MethodPost, resourcesPath("hosts", host.ID, "apps"), map[string]any{"apps": rows})
}

// comfyQueue reads ComfyUI's /queue: each entry is [number, prompt_id,
// prompt, ...], and the prompt's node class types tell training, image and
// video work apart.
func comfyQueue(body []byte, row map[string]any) error {
	var queue struct {
		Running []json.RawMessage `json:"queue_running"`
		Pending []json.RawMessage `json:"queue_pending"`
	}
	if err := json.Unmarshal(body, &queue); err != nil {
		return fmt.Errorf("ComfyUI queue is not JSON: %w", err)
	}
	jobs := []map[string]any{}
	for _, group := range []struct {
		state   string
		entries []json.RawMessage
	}{{"running", queue.Running}, {"queued", queue.Pending}} {
		for _, entry := range group.entries {
			var fields []json.RawMessage
			if err := json.Unmarshal(entry, &fields); err != nil || len(fields) < 2 {
				return errors.New("unexpected ComfyUI queue entry")
			}
			var id any
			_ = json.Unmarshal(fields[1], &id)
			types := []string{}
			if len(fields) > 2 {
				var nodes map[string]struct {
					ClassType string `json:"class_type"`
				}
				if json.Unmarshal(fields[2], &nodes) == nil {
					keys := make([]string, 0, len(nodes))
					for key := range nodes {
						keys = append(keys, key)
					}
					slices.SortFunc(keys, compareNodeIDs)
					for _, key := range keys {
						kind := firstNonEmptyString(nodes[key].ClassType, "unknown")
						if !slices.Contains(types, kind) {
							types = append(types, kind)
						}
					}
				}
			}
			jobs = append(jobs, map[string]any{"id": fmt.Sprint(id), "state": group.state, "node_types": types})
		}
	}
	row["online"], row["running"], row["queued"], row["jobs"] = true, len(queue.Running), len(queue.Pending), jobs
	return nil
}

// compareNodeIDs orders ComfyUI node IDs ("2" before "10") so node types are
// listed in workflow order.
func compareNodeIDs(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

// workbench mirrors saved Workbench request files. A file's modification time
// is the observation time, so rereading an unchanged file never renews it, and
// a terminal file only closes a job this ledger already shows as active.
func (c *resourcesCollector) workbench(ctx context.Context) {
	if len(c.config.WorkbenchRoots) == 0 {
		return
	}
	active := map[string]string{} // Workbench Id -> work row id
	body, err := c.client.Do(ctx, http.MethodGet, resourcesPath("work")+"?active=true&source=workbench", nil)
	if err != nil {
		c.warn("reading active Workbench rows: %v", err)
		return
	}
	var listing struct {
		Work []struct {
			ID         string  `json:"id"`
			ExternalID *string `json:"external_id"`
		} `json:"work"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		c.warn("decoding active Workbench rows: %v", err)
		return
	}
	for _, row := range listing.Work {
		if row.ExternalID != nil {
			active[*row.ExternalID] = row.ID
		}
	}
	for _, root := range c.config.WorkbenchRoots {
		files, _ := filepath.Glob(filepath.Join(root, "*", "status.json"))
		for _, file := range files {
			c.workbenchFile(ctx, file, active)
		}
	}
}

func (c *resourcesCollector) workbenchFile(ctx context.Context, file string, active map[string]string) {
	info, err := os.Stat(file)
	if err != nil {
		return
	}
	body, err := os.ReadFile(file)
	if err != nil {
		return
	}
	var saved struct {
		ID        string   `json:"Id"`
		Status    *int     `json:"Status"`
		Operation string   `json:"Operation"`
		Requester string   `json:"Requester"`
		Resources []string `json:"Resources"`
	}
	if json.Unmarshal(body, &saved) != nil || saved.ID == "" || saved.Status == nil {
		return
	}
	observed := info.ModTime().UTC()
	if *saved.Status == 0 || *saved.Status == 1 {
		link := resourcesWorkbenchLink{Host: "den-agents", Resources: []string{}}
		for _, label := range saved.Resources {
			if found, ok := c.config.WorkbenchResources[label]; ok {
				link = found
				break
			}
		}
		state := map[int]string{0: "queued", 1: "running"}[*saved.Status]
		c.send(ctx, http.MethodPost, resourcesPath("work"), map[string]any{
			"source": "workbench", "external_id": saved.ID, "host": link.Host, "resources": nonNilStrings(link.Resources),
			"title": firstNonEmptyString(saved.Operation, "Workbench job") + " · " + saved.ID,
			"owner": firstNonEmptyString(saved.Requester, "workbench"), "state": state,
			"note":        "Saved Workbench state; worker liveness is not verified here. " + file,
			"observed_at": observed, "ttl_seconds": workbenchTTLSeconds,
		})
		return
	}
	row, ok := active[saved.ID]
	if !ok {
		return
	}
	state := map[int]string{2: "completed", 3: "failed", 4: "cancelled"}[*saved.Status]
	c.send(ctx, http.MethodPost, resourcesPath("work", row), map[string]any{
		"state": firstNonEmptyString(state, "failed"), "observed_at": observed,
	})
}

func (c *resourcesCollector) send(ctx context.Context, method string, path string, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		c.warn("encoding %s: %v", path, err)
		return
	}
	if c.dryRun {
		c.mu.Lock()
		c.writes = append(c.writes, collectWrite{Method: method, Path: path, Body: body})
		c.mu.Unlock()
		return
	}
	if _, err := c.client.Do(ctx, method, path, body); err != nil && ctx.Err() == nil {
		c.warn("%s %s: %v", method, path, err)
	}
}

func (c *resourcesCollector) warn(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = fmt.Fprintf(c.stderr, "den-tool resources collect: "+format+"\n", args...)
}

func tail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
