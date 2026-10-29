package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The collector turns evidence that backup jobs already leave behind (report
// files, the systemd journal, Proxmox task logs) into Ops reports. It never
// starts, stops or retries a job; it only reads and reports.

const (
	opsCollectDefaultLookback = 14 * 24 * time.Hour
	opsCollectCacheRetention  = 60 * 24 * time.Hour
	opsCollectRemoteTimeout   = 2 * time.Minute
)

type opsCollectConfig struct {
	// CacheFile remembers what was already delivered or spooled, so repeated
	// collections only send what changed.
	CacheFile string `json:"cache_file"`
	// Lookback bounds how far back sources look for runs, as a Go duration.
	Lookback string `json:"lookback"`
	// Hosts and Jobs are registered on every collection, in the shape
	// PUT /v1/ops/hosts/{id} and PUT /v1/ops/jobs/{id} take.
	Hosts   map[string]json.RawMessage `json:"hosts"`
	Jobs    map[string]json.RawMessage `json:"jobs"`
	Sources []opsCollectSource         `json:"sources"`
}

type opsCollectSource struct {
	Type string `json:"type"`
	Name string `json:"name"`
	// StateDir is the local report directory of a borg-runner or thin-config job.
	StateDir string `json:"state_dir"`
	// MaintenanceJob receives a borg-runner's maintenance runs, so a weekly
	// check never counts as an attempt of the daily backup.
	MaintenanceJob string `json:"maintenance_job"`
	// NASCopyJob receives a thin-config run's NAS recovery copy and its checks.
	NASCopyJob string `json:"nas_copy_job"`
	// SSH is the command that reaches a remote host, ending with the host
	// alias; the remote command is appended as one argument.
	SSH []string `json:"ssh"`
	// JobID, HostID and Units describe a snapraid source.
	JobID  string   `json:"job_id"`
	HostID string   `json:"host_id"`
	Units  []string `json:"units"`
	// Node is the Proxmox node name (which can differ from the host ID).
	Node string `json:"node"`
	// TaskLimit is how many recent vzdump tasks a proxmox-vzdump source reads.
	TaskLimit int `json:"task_limit"`
}

// opsCollectWrite is one PUT the collector wants Ops to have.
type opsCollectWrite struct {
	Path string
	Body []byte
	// Done, when set, is recorded in the cache once this write is delivered
	// or spooled, so a source can skip work it has fully reported.
	Done string
	// Always skips the cache: registrations are cheap and resending them
	// restores a job's expectation even if the Ops ledger was replaced.
	Always bool
}

type opsCollectEnv struct {
	now      time.Time
	lookback time.Duration
	cache    *opsCollectCache
	remote   func(ctx context.Context, ssh []string, command string) ([]byte, error)
	// registeredJobs lists the jobs Ops has now, so sources can retire
	// coverage that disappeared. Nil skips that reconciliation.
	registeredJobs func(ctx context.Context) ([]opsRegisteredJob, error)
}

type opsCollectSummary struct {
	Sources   int      `json:"sources"`
	Writes    int      `json:"writes"`
	Delivered int      `json:"delivered"`
	Spooled   int      `json:"spooled"`
	Unchanged int      `json:"unchanged"`
	Rejected  []string `json:"rejected"`
	Failures  []string `json:"failures"`
}

func runOpsCollect(args []string, stdout, stderr io.Writer) int {
	configPath, dryRun, resend, asJSON, err := parseOpsCollectArgs(args)
	if err != nil {
		return opsUsageError(stderr, err.Error())
	}
	config, err := loadOpsCollectConfig(configPath)
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	client, err := OpsClientFromEnv()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	spool, err := OpsSpoolFromEnv()
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	cache, err := loadOpsCollectCache(config.CacheFile)
	if err != nil {
		return writeRuntimeError(stderr, err)
	}
	if resend {
		cache.Entries = map[string]opsCollectCacheEntry{}
	}
	lookback, err := time.ParseDuration(config.Lookback)
	if err != nil {
		return writeRuntimeError(stderr, fmt.Errorf("lookback: %w", err))
	}
	env := opsCollectEnv{
		now: time.Now().UTC(), lookback: lookback, cache: cache, remote: runRemoteCommand,
		registeredJobs: func(ctx context.Context) ([]opsRegisteredJob, error) {
			body, err := client.Do(ctx, http.MethodGet, "/v1/ops/jobs", nil)
			if err != nil {
				return nil, err
			}
			var response struct {
				Jobs []opsRegisteredJob `json:"jobs"`
			}
			if err := json.Unmarshal(body, &response); err != nil {
				return nil, fmt.Errorf("decode ops jobs: %w", err)
			}
			return response.Jobs, nil
		},
	}
	ctx := context.Background()
	writes, failures := collectOpsEvidence(ctx, config, env)
	for _, failure := range failures {
		_, _ = fmt.Fprintf(stderr, "den-tool: warning: %s\n", failure)
	}
	if dryRun {
		for _, write := range writes {
			var body bytes.Buffer
			if json.Compact(&body, write.Body) != nil {
				body.Write(write.Body)
			}
			_, _ = fmt.Fprintf(stdout, "PUT %s\n%s\n", write.Path, body.Bytes())
		}
		return 0
	}
	summary := deliverOpsWrites(ctx, client, spool, cache, writes, env.now, stderr)
	summary.Sources = len(config.Sources)
	summary.Failures = append(append([]string{}, failures...), summary.Failures...)
	if err := cache.save(env.now); err != nil {
		summary.Failures = append(summary.Failures, err.Error())
		_, _ = fmt.Fprintf(stderr, "den-tool: warning: %v\n", err)
	}
	if asJSON {
		_ = writeJSON(stdout, summary)
	} else {
		_, _ = fmt.Fprintf(stdout, "collected %d writes from %d sources: %d delivered, %d spooled, %d unchanged, %d rejected, %d source failures\n",
			summary.Writes, summary.Sources, summary.Delivered, summary.Spooled, summary.Unchanged, len(summary.Rejected), len(failures))
	}
	if len(summary.Failures) > 0 || len(summary.Rejected) > 0 {
		return 1
	}
	return 0
}

func parseOpsCollectArgs(args []string) (configPath string, dryRun bool, resend bool, asJSON bool, err error) {
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--config":
			if index+1 >= len(args) {
				return "", false, false, false, errors.New("--config needs a file")
			}
			index++
			configPath = args[index]
		case "--dry-run":
			dryRun = true
		case "--resend":
			resend = true
		case "--json":
			asJSON = true
		default:
			return "", false, false, false, fmt.Errorf("unknown collect argument %q", args[index])
		}
	}
	if configPath == "" {
		configPath = os.Getenv("DEN_OPS_COLLECT_CONFIG")
	}
	if configPath == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", false, false, false, errors.New("collect needs --config")
		}
		configPath = filepath.Join(home, ".config", "den-ops-collect", "config.json")
	}
	return configPath, dryRun, resend, asJSON, nil
}

func loadOpsCollectConfig(path string) (opsCollectConfig, error) {
	var config opsCollectConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return config, fmt.Errorf("read collect config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("parse collect config %s: %w", path, err)
	}
	if config.Lookback == "" {
		config.Lookback = opsCollectDefaultLookback.String()
	}
	if config.CacheFile == "" {
		spool, err := OpsSpoolFromEnv()
		if err != nil {
			return config, err
		}
		config.CacheFile = filepath.Join(filepath.Dir(spool.Dir()), "ops-collect-cache.json")
	}
	for index, source := range config.Sources {
		if err := source.validate(); err != nil {
			return config, fmt.Errorf("source %d: %w", index, err)
		}
	}
	return config, nil
}

func (s opsCollectSource) validate() error {
	switch s.Type {
	case "borg-runner", "thin-config":
		if s.StateDir == "" {
			return fmt.Errorf("%s needs state_dir", s.Type)
		}
	case "snapraid":
		if len(s.SSH) == 0 || s.JobID == "" || s.HostID == "" || len(s.Units) == 0 {
			return errors.New("snapraid needs ssh, job_id, host_id and units")
		}
	case "proxmox-vzdump":
		if len(s.SSH) == 0 || s.Node == "" || s.HostID == "" {
			return errors.New("proxmox-vzdump needs ssh, node and host_id")
		}
	default:
		return fmt.Errorf("unknown source type %q", s.Type)
	}
	return nil
}

func (s opsCollectSource) label() string {
	if s.Name != "" {
		return s.Name
	}
	return s.Type
}

// collectOpsEvidence gathers every write: registrations first, so runs and
// checks always find their job, then each source's evidence. A source that
// fails is reported and skipped; the others still report.
func collectOpsEvidence(ctx context.Context, config opsCollectConfig, env opsCollectEnv) ([]opsCollectWrite, []string) {
	var writes []opsCollectWrite
	var failures []string
	for _, id := range sortedKeys(config.Hosts) {
		writes = append(writes, opsCollectWrite{Path: opsPath("hosts", id), Body: config.Hosts[id], Always: true})
	}
	for _, id := range sortedKeys(config.Jobs) {
		writes = append(writes, opsCollectWrite{Path: opsPath("jobs", id), Body: config.Jobs[id], Always: true})
	}
	var evidence []opsCollectWrite
	for _, source := range config.Sources {
		var collected []opsCollectWrite
		var err error
		switch source.Type {
		case "borg-runner":
			collected, err = collectBorgRunner(source, env)
		case "thin-config":
			collected, err = collectThinConfig(source, env)
		case "snapraid":
			collected, err = collectSnapraid(ctx, source, env)
		case "proxmox-vzdump":
			collected, err = collectProxmox(ctx, source, env, config.Hosts)
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", source.label(), err))
		}
		for _, write := range collected {
			// Derived registrations go before any evidence that needs them.
			if strings.HasPrefix(write.Path, "/v1/ops/hosts/") || !strings.Contains(strings.TrimPrefix(write.Path, "/v1/ops/jobs/"), "/") {
				write.Always = true
				writes = append(writes, write)
			} else {
				evidence = append(evidence, write)
			}
		}
	}
	return append(writes, evidence...), failures
}

// deliverOpsWrites replays the spool, then sends each changed write. Once Ops
// is unreachable the rest go straight to the spool, in order.
func deliverOpsWrites(ctx context.Context, client *OpsClient, spool *OpsSpool, cache *opsCollectCache, writes []opsCollectWrite, now time.Time, stderr io.Writer) opsCollectSummary {
	summary := opsCollectSummary{Writes: len(writes), Rejected: []string{}, Failures: []string{}}
	replay := spool.Replay(ctx, client)
	offline := replay.Error != ""
	if offline {
		_, _ = fmt.Fprintf(stderr, "den-tool: warning: ops unreachable (%s); spooling this collection\n", replay.Error)
	}
	for _, write := range writes {
		key, hash := "PUT "+write.Path, opsHash(write.Body)
		if !write.Always && cache.unchanged(key, hash) {
			summary.Unchanged++
			cache.markDone(write.Done, now)
			continue
		}
		if !offline {
			_, err := client.Do(ctx, http.MethodPut, write.Path, write.Body)
			var httpErr *OpsHTTPError
			switch {
			case err == nil:
				summary.Delivered++
				cache.record(key, hash, now)
				cache.markDone(write.Done, now)
				continue
			case errors.As(err, &httpErr) && httpErr.Permanent():
				summary.Rejected = append(summary.Rejected, fmt.Sprintf("PUT %s: %v", write.Path, err))
				_, _ = fmt.Fprintf(stderr, "den-tool: ops rejected PUT %s: %v\n", write.Path, err)
				continue
			default:
				offline = true
				_, _ = fmt.Fprintf(stderr, "den-tool: warning: %v; spooling the rest of this collection\n", err)
			}
		}
		if _, err := spool.Add(http.MethodPut, write.Path, write.Body, now); err != nil {
			summary.Failures = append(summary.Failures, fmt.Sprintf("spool PUT %s: %v", write.Path, err))
			continue
		}
		summary.Spooled++
		cache.record(key, hash, now)
		cache.markDone(write.Done, now)
	}
	return summary
}

type opsCollectCacheEntry struct {
	Hash string    `json:"hash,omitempty"`
	At   time.Time `json:"at"`
}

type opsCollectCache struct {
	path    string
	Entries map[string]opsCollectCacheEntry `json:"entries"`
}

func loadOpsCollectCache(path string) (*opsCollectCache, error) {
	cache := &opsCollectCache{path: path, Entries: map[string]opsCollectCacheEntry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cache, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read collect cache: %w", err)
	}
	if err := json.Unmarshal(data, cache); err != nil || cache.Entries == nil {
		// A damaged cache only costs resending what Ops already has.
		cache.Entries = map[string]opsCollectCacheEntry{}
	}
	return cache, nil
}

func (c *opsCollectCache) unchanged(key string, hash string) bool {
	entry, ok := c.Entries[key]
	return ok && entry.Hash == hash
}

func (c *opsCollectCache) record(key string, hash string, now time.Time) {
	c.Entries[key] = opsCollectCacheEntry{Hash: hash, At: now}
}

func (c *opsCollectCache) markDone(marker string, now time.Time) {
	if marker != "" {
		c.Entries["done "+marker] = opsCollectCacheEntry{At: now}
	}
}

func (c *opsCollectCache) isDone(marker string) bool {
	_, ok := c.Entries["done "+marker]
	return ok
}

func (c *opsCollectCache) save(now time.Time) error {
	for key, entry := range c.Entries {
		if now.Sub(entry.At) > opsCollectCacheRetention {
			delete(c.Entries, key)
		}
	}
	data, err := json.MarshalIndent(c, "", " ")
	if err != nil {
		return fmt.Errorf("encode collect cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o750); err != nil {
		return fmt.Errorf("create collect cache directory: %w", err)
	}
	temporary := c.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o640); err != nil {
		return fmt.Errorf("write collect cache: %w", err)
	}
	if err := os.Rename(temporary, c.path); err != nil {
		return fmt.Errorf("commit collect cache: %w", err)
	}
	return nil
}

func opsHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func runRemoteCommand(ctx context.Context, ssh []string, command string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, opsCollectRemoteTimeout)
	defer cancel()
	arguments := append(slices.Clone(ssh[1:]), command)
	process := exec.CommandContext(ctx, ssh[0], arguments...)
	var stderr bytes.Buffer
	process.Stderr = &stderr
	output, err := process.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", ssh[0], err, firstLine(strings.TrimSpace(stderr.String())))
	}
	return output, nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode collector report: %v", err))
	}
	return data
}

// compactUTC formats a time as 20261029T043010Z, for stable IDs.
func compactUTC(value time.Time) string {
	return value.UTC().Format("20060102T150405Z")
}
