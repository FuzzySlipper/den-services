package broker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Runner struct {
	cfg        *Config
	httpClient *http.Client
	clock      func() time.Time
}

func NewRunner(cfg *Config) *Runner {
	return &Runner{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeouts.HealthTimeout,
		},
		clock: func() time.Time { return time.Now().UTC() },
	}
}

func (r *Runner) Run(ctx context.Context, options RunOptions) (RunResult, error) {
	if strings.TrimSpace(options.Project) == "" {
		return RunResult{}, errors.New("project is required")
	}
	repoRoot, err := resolveRepoRoot(options.RepoRoot)
	if err != nil {
		return RunResult{}, err
	}
	manifestPath, err := FindManifest(repoRoot, options.ManifestPath)
	if err != nil {
		return RunResult{}, err
	}
	manifest, err := LoadManifest(manifestPath, repoRoot, r.cfg)
	if err != nil {
		return RunResult{}, err
	}
	if manifest.Project != options.Project {
		return RunResult{}, fmt.Errorf("%w: requested project %q but manifest project is %q", ErrInvalidManifest, options.Project, manifest.Project)
	}
	runID := newRunID(manifest.Project, r.clock())
	artifactRoot := filepath.Join(r.cfg.ArtifactRoot, manifest.Project, runID)
	if err := os.MkdirAll(artifactRoot, 0o700); err != nil {
		return RunResult{}, fmt.Errorf("creating artifact root: %w", err)
	}
	evidence := Evidence{
		SchemaVersion:           SchemaVersion,
		RunID:                   runID,
		Project:                 manifest.Project,
		RepoRoot:                repoRoot,
		StartedAt:               r.clock(),
		Status:                  "running",
		HumanInspectionRequired: manifest.Tests.ArtifactPolicy.RequiresHumanInspection(),
		Artifacts: ArtifactEvidence{
			Root:      artifactRoot,
			IndexPath: filepath.Join(artifactRoot, "run-index.json"),
		},
	}
	if options.DenProjectID != "" || options.DenTaskID > 0 {
		evidence.Den = &DenEvidence{ProjectID: options.DenProjectID, TaskID: options.DenTaskID}
	}
	registry := NewLeaseRegistry(r.cfg.StateDir)
	if err := registry.Lock(ctx, r.cfg.Timeouts.LockTimeout); err != nil {
		return RunResult{}, err
	}
	defer registry.Unlock()
	leases, err := registry.Load()
	if err != nil {
		return RunResult{}, err
	}
	leases, warnings := pruneDeadLeases(leases)
	evidence.Warnings = append(evidence.Warnings, warnings...)
	server, leases, err := r.prepareServer(ctx, manifest, leases, runID, artifactRoot)
	evidence.Server = server.evidence
	if err != nil {
		evidence.Status = "failed"
		evidence.FinishedAt = r.clock()
		_ = finalizeEvidence(evidence.Artifacts.IndexPath, &evidence)
		_ = registry.Save(leases)
		return RunResult{Evidence: evidence}, err
	}
	if !server.reused {
		leases = upsertLease(leases, server.lease)
		if err := registry.Save(leases); err != nil {
			_ = stopManagedProcess(server.process, r.cfg.Timeouts.ShutdownTimeout)
			return RunResult{}, err
		}
	}
	defer func() {
		if !server.reused && server.process != nil {
			_ = stopManagedProcess(server.process, r.cfg.Timeouts.ShutdownTimeout)
			leases, _ = registry.Load()
			_ = registry.Save(removeLease(leases, runID))
		}
	}()

	playwright, runErr := r.runPlaywright(ctx, manifest, options, artifactRoot, server.evidence.BaseURL)
	evidence.Playwright = playwright
	if runErr != nil {
		evidence.Status = "failed"
	} else {
		evidence.Status = "passed"
	}
	if !server.reused && server.process != nil {
		if err := stopManagedProcess(server.process, r.cfg.Timeouts.ShutdownTimeout); err == nil {
			evidence.Server.StoppedOwnedPID = true
		}
		leases, _ = registry.Load()
		_ = registry.Save(removeLease(leases, runID))
		server.process = nil
	}
	evidence.FinishedAt = r.clock()
	if err := finalizeEvidence(evidence.Artifacts.IndexPath, &evidence); err != nil && runErr == nil {
		runErr = err
	}
	return RunResult{Evidence: evidence}, runErr
}

type preparedServer struct {
	evidence ServerEvidence
	lease    LeaseRecord
	process  *ManagedProcess
	reused   bool
}

func (r *Runner) prepareServer(ctx context.Context, manifest *Manifest, leases []LeaseRecord, runID string, artifactRoot string) (preparedServer, []LeaseRecord, error) {
	host := manifest.Serve.Host
	port, reused, reuseSource, health, err := r.selectPortOrReuse(ctx, manifest, leases)
	if err != nil {
		return preparedServer{}, leases, err
	}
	baseURL := fmt.Sprintf("http://%s:%d", host, port)
	values := templateValues(manifest.Project, manifest.RepoRoot, host, port, baseURL, artifactRoot)
	command := renderTemplate(manifest.Serve.Command, values)
	evidence := ServerEvidence{
		Host:        host,
		Port:        port,
		BaseURL:     baseURL,
		Command:     command,
		Reused:      reused,
		ReuseSource: reuseSource,
		Health:      health,
		StdoutLog:   filepath.Join(artifactRoot, "server.stdout.log"),
		StderrLog:   filepath.Join(artifactRoot, "server.stderr.log"),
	}
	if reused {
		return preparedServer{evidence: evidence, reused: true}, leases, nil
	}
	env := map[string]string{
		"HOST":                            host,
		"PORT":                            strconv.Itoa(port),
		"BASE_URL":                        baseURL,
		"PLAYWRIGHT_BROKER_BASE_URL":      baseURL,
		"PLAYWRIGHT_BROKER_ARTIFACT_ROOT": artifactRoot,
	}
	for key, value := range manifest.Serve.Environment {
		env[key] = renderTemplate(value, values)
	}
	process, err := startManagedProcess(ctx, command, manifest.RepoRoot, env, evidence.StdoutLog, evidence.StderrLog)
	if err != nil {
		return preparedServer{}, leases, err
	}
	evidence.OwnedPID = process.PID
	health, err = waitForHealth(ctx, r.httpClient, manifest, port, manifest.Serve.StartupTimeout, manifest.Serve.HealthInterval)
	evidence.Health = health
	if err != nil {
		_ = stopManagedProcess(process, r.cfg.Timeouts.ShutdownTimeout)
		return preparedServer{evidence: evidence, process: process}, leases, err
	}
	lease := LeaseRecord{
		RunID:     runID,
		Project:   manifest.Project,
		RepoRoot:  manifest.RepoRoot,
		Host:      host,
		Port:      port,
		BaseURL:   baseURL,
		Command:   command,
		PID:       process.PID,
		StartedAt: r.clock(),
	}
	return preparedServer{evidence: evidence, lease: lease, process: process}, leases, nil
}

func (r *Runner) selectPortOrReuse(ctx context.Context, manifest *Manifest, leases []LeaseRecord) (int, bool, string, HealthEvidence, error) {
	blocked := make(map[int]bool)
	for _, lease := range leases {
		if lease.Host == manifest.Serve.Host && processAlive(lease.PID) {
			blocked[lease.Port] = true
		}
	}
	if manifest.Serve.PreferredPort > 0 {
		port := manifest.Serve.PreferredPort
		if portInUse(manifest.Serve.Host, port, r.cfg.Timeouts.HealthTimeout) {
			health := checkHealth(ctx, r.httpClient, manifest, port)
			lease := findLeaseForPort(leases, manifest.Serve.Host, port)
			if health.Matched && manifest.Serve.ReusePolicy != ReusePolicyNever {
				if lease != nil && processAlive(lease.PID) {
					return port, true, "broker_owned", health, nil
				}
				if manifest.Serve.ReusePolicy == ReusePolicyExplicit {
					return port, true, "explicit", health, nil
				}
			}
			blocked[port] = true
		} else {
			return port, false, "", HealthEvidence{URL: manifest.HealthURL(port)}, nil
		}
	}
	portRange := manifest.PortRange(r.cfg)
	port, err := findFreePort(manifest.Serve.Host, portRange, blocked)
	if err != nil {
		return 0, false, "", HealthEvidence{}, err
	}
	return port, false, "", HealthEvidence{URL: manifest.HealthURL(port)}, nil
}

func (r *Runner) runPlaywright(ctx context.Context, manifest *Manifest, options RunOptions, artifactRoot string, baseURL string) (PlaywrightEvidence, error) {
	args := playwrightArgs(manifest, options)
	values := templateValues(manifest.Project, manifest.RepoRoot, manifest.Serve.Host, 0, baseURL, artifactRoot)
	command := renderTemplate(manifest.Tests.Command, values)
	fullCommand := command
	if len(args) > 0 {
		quoted := make([]string, 0, len(args))
		for _, arg := range args {
			quoted = append(quoted, shellQuote(arg))
		}
		fullCommand += " " + strings.Join(quoted, " ")
	}
	stdoutPath := filepath.Join(artifactRoot, "playwright.stdout.log")
	stderrPath := filepath.Join(artifactRoot, "playwright.stderr.log")
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		return PlaywrightEvidence{}, fmt.Errorf("creating playwright stdout log: %w", err)
	}
	defer stdoutFile.Close()
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		return PlaywrightEvidence{}, fmt.Errorf("creating playwright stderr log: %w", err)
	}
	defer stderrFile.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeouts.RunTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "sh", "-c", fullCommand)
	cmd.Dir = manifest.RepoRoot
	env := map[string]string{
		"BASE_URL":                        baseURL,
		"PLAYWRIGHT_BROKER_BASE_URL":      baseURL,
		"PLAYWRIGHT_BROKER_ARTIFACT_ROOT": artifactRoot,
		"PLAYWRIGHT_BROKER_EVIDENCE_PATH": filepath.Join(artifactRoot, "run-index.json"),
	}
	for key, value := range manifest.Tests.Environment {
		env[key] = renderTemplate(value, values)
	}
	cmd.Env = mergeEnv(os.Environ(), env)
	cmd.Stdout = io.MultiWriter(stdoutFile, &stdout)
	cmd.Stderr = io.MultiWriter(stderrFile, &stderr)
	startedAt := r.clock()
	err = cmd.Run()
	duration := r.clock().Sub(startedAt)
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	evidence := PlaywrightEvidence{
		Command:       command,
		Args:          args,
		ExitCode:      exitCode,
		Duration:      duration,
		StdoutLog:     stdoutPath,
		StderrLog:     stderrPath,
		StdoutExcerpt: excerpt(stdout.Bytes(), 4096),
		StderrExcerpt: excerpt(stderr.Bytes(), 4096),
	}
	if err != nil {
		return evidence, fmt.Errorf("playwright command failed: %w", err)
	}
	return evidence, nil
}

func playwrightArgs(manifest *Manifest, options RunOptions) []string {
	args := append([]string(nil), manifest.Tests.DefaultArgs...)
	if manifest.Tests.ConfigPath != "" {
		args = append(args, "--config", manifest.Tests.ConfigPath)
	}
	if options.Grep != "" {
		args = append(args, "--grep", options.Grep)
	}
	if options.Headed {
		args = append(args, "--headed")
	}
	if options.PlaywrightProject != "" {
		args = append(args, "--project", options.PlaywrightProject)
	}
	if options.Test != "" {
		args = append(args, options.Test)
	}
	args = append(args, options.ExtraArgs...)
	return args
}

func pruneDeadLeases(leases []LeaseRecord) ([]LeaseRecord, []string) {
	var warnings []string
	filtered := leases[:0]
	for _, lease := range leases {
		if processAlive(lease.PID) {
			filtered = append(filtered, lease)
			continue
		}
		warnings = append(warnings, fmt.Sprintf("removed stale lease %s for pid %d", lease.RunID, lease.PID))
	}
	return filtered, warnings
}

func finalizeEvidence(path string, evidence *Evidence) error {
	files, err := listArtifactFiles(evidence.Artifacts.Root)
	if err != nil {
		return err
	}
	evidence.Artifacts.Files = files
	return writeEvidence(path, *evidence)
}

func resolveRepoRoot(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getting current directory: %w", err)
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving repo root: %w", err)
	}
	return filepath.Clean(abs), nil
}

func newRunID(project string, now time.Time) string {
	safeProject := strings.NewReplacer("/", "-", " ", "-").Replace(project)
	return fmt.Sprintf("%s-%s-%d", safeProject, now.UTC().Format("20060102T150405.000000000Z"), os.Getpid())
}

func renderTemplate(template string, values map[string]string) string {
	result := template
	for key, value := range values {
		result = strings.ReplaceAll(result, "{"+key+"}", value)
	}
	return result
}

func templateValues(project string, repoRoot string, host string, port int, baseURL string, artifactRoot string) map[string]string {
	return map[string]string{
		"project":       project,
		"repo_root":     repoRoot,
		"host":          host,
		"port":          strconv.Itoa(port),
		"base_url":      baseURL,
		"artifact_root": artifactRoot,
	}
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
