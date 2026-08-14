package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type PlaytestManager struct {
	cfg        *Config
	httpClient *http.Client
	clock      func() time.Time
}

func NewPlaytestManager(cfg *Config) *PlaytestManager {
	return &PlaytestManager{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: cfg.Playtest.CommandTimeout},
		clock:      func() time.Time { return time.Now().UTC() },
	}
}

func (m *PlaytestManager) Start(ctx context.Context, options PlaytestStartOptions) (PlaytestSession, error) {
	if m.cfg.Playtest.NodeCommand == "" || m.cfg.Playtest.DriverScript == "" || m.cfg.Playtest.DriverStartupTimeout <= 0 || m.cfg.Playtest.CommandTimeout <= 0 {
		return PlaytestSession{}, errors.New("playtest configuration is incomplete")
	}
	if strings.TrimSpace(options.Project) == "" {
		return PlaytestSession{}, errors.New("project is required")
	}
	repoRoot, err := resolveRepoRoot(options.RepoRoot)
	if err != nil {
		return PlaytestSession{}, err
	}
	manifestPath, err := FindManifest(repoRoot, options.ManifestPath)
	if err != nil {
		return PlaytestSession{}, err
	}
	manifest, err := LoadManifest(manifestPath, repoRoot, m.cfg)
	if err != nil {
		return PlaytestSession{}, err
	}
	warnings := []string{}
	if manifest.Project != options.Project {
		warnings = append(warnings, fmt.Sprintf("requested project %q differs from manifest project %q; manifest identity used", options.Project, manifest.Project))
	}

	sessionID := newRunID(manifest.Project+"-playtest", m.clock())
	artifactRoot := filepath.Join(m.cfg.ArtifactRoot, manifest.Project, sessionID)
	if err := os.MkdirAll(artifactRoot, 0o700); err != nil {
		return PlaytestSession{}, fmt.Errorf("creating playtest artifact root: %w", err)
	}

	registry := NewLeaseRegistry(m.cfg.StateDir)
	if err := registry.Lock(ctx, m.cfg.Timeouts.LockTimeout); err != nil {
		return PlaytestSession{}, err
	}
	defer registry.Unlock()
	leases, err := registry.Load()
	if err != nil {
		return PlaytestSession{}, err
	}
	leases, leaseWarnings := pruneDeadLeases(leases)
	warnings = append(warnings, leaseWarnings...)
	runner := NewRunner(m.cfg)
	server, leases, err := runner.prepareServer(context.Background(), manifest, leases, sessionID, artifactRoot)
	if err != nil {
		return PlaytestSession{}, err
	}
	if !server.reused {
		leases = upsertLease(leases, server.lease)
		if err := registry.Save(leases); err != nil {
			_ = stopManagedProcess(server.process, m.cfg.Timeouts.ShutdownTimeout)
			return PlaytestSession{}, err
		}
	}

	driverPort, err := reservePort()
	if err != nil {
		m.cleanupStartedServer(registry, leases, sessionID, server)
		return PlaytestSession{}, err
	}
	viewport := manifest.Playtest.Viewport
	if options.Viewport != nil {
		viewport = *options.Viewport
	}
	headed := manifest.Playtest.Headed
	if options.Headed != nil {
		headed = *options.Headed
	}
	recordVideo := manifest.Playtest.RecordVideo
	if options.RecordVideo != nil {
		recordVideo = *options.RecordVideo
	}
	startURL := strings.TrimRight(server.evidence.BaseURL, "/") + "/" + strings.TrimLeft(manifest.Playtest.StartPath, "/")
	driverOptions := map[string]any{
		"sessionId":    sessionID,
		"project":      manifest.Project,
		"repoRoot":     repoRoot,
		"baseURL":      server.evidence.BaseURL,
		"startURL":     startURL,
		"artifactRoot": artifactRoot,
		"port":         driverPort,
		"owner":        options.Owner,
		"scenario":     options.Scenario,
		"headed":       headed,
		"viewport":     viewport,
		"recordVideo":  recordVideo,
		"inputHelper":  m.cfg.Playtest.InputHelper,
		"metadata":     options.Metadata,
	}
	if options.DenProjectID != "" || options.DenTaskID > 0 {
		driverOptions["den"] = map[string]any{"project_id": options.DenProjectID, "task_id": options.DenTaskID}
	}
	driverPID, err := m.startDriver(driverOptions, manifest.Playtest.Environment, artifactRoot)
	if err != nil {
		m.cleanupStartedServer(registry, leases, sessionID, server)
		return PlaytestSession{}, err
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", driverPort)
	if err := m.waitForDriver(ctx, endpoint, driverPID); err != nil {
		_ = stopProcessGroup(driverPID, m.cfg.Timeouts.ShutdownTimeout)
		m.cleanupStartedServer(registry, leases, sessionID, server)
		return PlaytestSession{}, err
	}

	session := PlaytestSession{
		SchemaVersion: PlaytestSchemaVersion,
		SessionID:     sessionID,
		Project:       manifest.Project,
		RepoRoot:      repoRoot,
		Owner:         options.Owner,
		Scenario:      options.Scenario,
		Status:        "running",
		StartedAt:     m.clock(),
		Endpoint:      endpoint,
		DriverPID:     driverPID,
		ServerPID:     server.evidence.OwnedPID,
		ServerReused:  server.reused,
		BaseURL:       server.evidence.BaseURL,
		ArtifactRoot:  artifactRoot,
		IndexPath:     filepath.Join(artifactRoot, "playtest-index.json"),
		Warnings:      warnings,
	}
	if boolMetadata(options.Metadata, "verbose_trace", "verboseTrace") {
		session.DecisionTracePath = filepath.Join(artifactRoot, "decision-trace.jsonl")
	}
	session.StatePath = m.sessionPath(sessionID)
	if err := m.saveSession(session); err != nil {
		_ = stopProcessGroup(driverPID, m.cfg.Timeouts.ShutdownTimeout)
		m.cleanupStartedServer(registry, leases, sessionID, server)
		return PlaytestSession{}, err
	}
	return session, nil
}

func (m *PlaytestManager) Call(ctx context.Context, sessionID string, request map[string]any) (map[string]any, error) {
	session, err := m.Get(sessionID)
	if err != nil {
		return nil, err
	}
	kind, _ := request["kind"].(string)
	if strings.TrimSpace(kind) == "" {
		request["kind"] = "inspect"
		request["kind_discrepancy"] = "missing kind interpreted as inspect"
		kind = "inspect"
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encoding playtest request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, session.Endpoint+"/command", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, callErr := m.httpClient.Do(httpRequest)
	var result map[string]any
	if callErr == nil {
		defer response.Body.Close()
		data, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			callErr = readErr
		} else if err := json.Unmarshal(data, &result); err != nil {
			callErr = fmt.Errorf("decoding playtest response: %w", err)
		}
	}
	if callErr != nil {
		diagnostic := map[string]any{"code": "driver_call_error", "error": callErr.Error(), "continued": processAlive(session.DriverPID)}
		result = map[string]any{
			"ok":            false,
			"continued":     diagnostic["continued"],
			"session_id":    session.SessionID,
			"index_path":    session.IndexPath,
			"result":        map[string]any{"partial": true, "error": callErr.Error()},
			"discrepancies": []map[string]any{diagnostic},
		}
		if persistErr := m.persistManagerCallFailure(session, request, result, diagnostic); persistErr != nil {
			result["discrepancies"] = append(result["discrepancies"].([]map[string]any), map[string]any{
				"code": "evidence_persist_error", "error": persistErr.Error(), "continued": false,
			})
		}
	}
	if kind == "finish" || kind == "cancel" {
		session.ExitInterview = firstPresent(request, "exit_interview", "exitInterview", "tester_feedback", "testerFeedback")
		if session.ExitInterview != nil {
			result["exit_interview"] = session.ExitInterview
		}
		finalStatus := kind
		if outcome, ok := request["outcome"].(string); ok && strings.TrimSpace(outcome) != "" {
			finalStatus = outcome
		}
		m.finishHostCleanup(&session, kind, finalStatus)
	}
	return result, nil
}

func (m *PlaytestManager) Get(sessionID string) (PlaytestSession, error) {
	data, err := os.ReadFile(m.sessionPath(sessionID))
	if err != nil {
		return PlaytestSession{}, fmt.Errorf("reading playtest session %s: %w", sessionID, err)
	}
	var session PlaytestSession
	if err := json.Unmarshal(data, &session); err != nil {
		return PlaytestSession{}, fmt.Errorf("parsing playtest session %s: %w", sessionID, err)
	}
	if session.Status == "running" && !processAlive(session.DriverPID) {
		session.Warnings = append(session.Warnings, "driver process is not alive; call results will contain recovery diagnostics")
	}
	return session, nil
}

func (m *PlaytestManager) List() ([]PlaytestSession, error) {
	dir := filepath.Join(m.cfg.StateDir, "playtest-sessions")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sessions := make([]PlaytestSession, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			continue
		}
		var session PlaytestSession
		if json.Unmarshal(data, &session) == nil {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (m *PlaytestManager) startDriver(options map[string]any, environment map[string]string, artifactRoot string) (int, error) {
	encoded, err := json.Marshal(options)
	if err != nil {
		return 0, err
	}
	stdoutPath := filepath.Join(artifactRoot, "driver.stdout.log")
	stderrPath := filepath.Join(artifactRoot, "driver.stderr.log")
	stdout, err := os.Create(stdoutPath)
	if err != nil {
		return 0, err
	}
	defer stdout.Close()
	stderr, err := os.Create(stderrPath)
	if err != nil {
		return 0, err
	}
	defer stderr.Close()
	cmd := exec.Command(m.cfg.Playtest.NodeCommand, m.cfg.Playtest.DriverScript)
	cmd.Dir = filepath.Dir(filepath.Dir(m.cfg.Playtest.DriverScript))
	values := map[string]string{
		"artifact_root": artifactRoot,
		"project":       stringValueFromAny(options["project"]),
		"repo_root":     stringValueFromAny(options["repoRoot"]),
		"base_url":      stringValueFromAny(options["baseURL"]),
		"session_id":    stringValueFromAny(options["sessionId"]),
	}
	env := map[string]string{"DEN_PLAYTEST_DRIVER_OPTIONS": string(encoded)}
	for key, value := range environment {
		env[key] = renderTemplate(value, values)
	}
	cmd.Env = mergeEnv(os.Environ(), env)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("starting playtest driver: %w", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		_ = stopProcessGroup(pid, m.cfg.Timeouts.ShutdownTimeout)
		return 0, fmt.Errorf("releasing playtest driver process: %w", err)
	}
	return pid, nil
}

func stringValueFromAny(value any) string {
	text, _ := value.(string)
	return text
}

func (m *PlaytestManager) waitForDriver(ctx context.Context, endpoint string, pid int) error {
	deadline := m.clock().Add(m.cfg.Playtest.DriverStartupTimeout)
	client := &http.Client{Timeout: m.cfg.Timeouts.HealthTimeout}
	for m.clock().Before(deadline) {
		if !processAlive(pid) {
			return errors.New("playtest driver exited during startup")
		}
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/health", nil)
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("playtest driver startup timed out after %s", m.cfg.Playtest.DriverStartupTimeout)
}

func (m *PlaytestManager) finishHostCleanup(session *PlaytestSession, kind string, finalStatus string) {
	diagnostics := []map[string]any{}
	driverDeadline := m.clock().Add(m.cfg.Timeouts.ShutdownTimeout)
	for processAlive(session.DriverPID) && m.clock().Before(driverDeadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if processGroupAlive(session.DriverPID) {
		if err := stopProcessGroup(session.DriverPID, m.cfg.Timeouts.ShutdownTimeout); err != nil {
			diagnostics = append(diagnostics, cleanupDiagnostic("driver_cleanup_error", err))
		}
		driverDeadline = m.clock().Add(m.cfg.Timeouts.ShutdownTimeout)
		for processGroupAlive(session.DriverPID) && m.clock().Before(driverDeadline) {
			time.Sleep(25 * time.Millisecond)
		}
	}
	serverStopped := session.ServerReused || session.ServerPID <= 0
	if !session.ServerReused && session.ServerPID > 0 {
		if err := stopProcessGroup(session.ServerPID, m.cfg.Timeouts.ShutdownTimeout); err != nil {
			diagnostics = append(diagnostics, cleanupDiagnostic("dev_server_cleanup_error", err))
			serverStopped = false
		} else {
			serverStopped = true
		}
	}
	registry := NewLeaseRegistry(m.cfg.StateDir)
	if err := registry.Lock(context.Background(), m.cfg.Timeouts.LockTimeout); err != nil {
		diagnostics = append(diagnostics, cleanupDiagnostic("lease_lock_error", err))
	} else {
		if leases, err := registry.Load(); err != nil {
			diagnostics = append(diagnostics, cleanupDiagnostic("lease_load_error", err))
		} else if err := registry.Save(removeLease(leases, session.SessionID)); err != nil {
			diagnostics = append(diagnostics, cleanupDiagnostic("lease_save_error", err))
		}
		if err := registry.Unlock(); err != nil {
			diagnostics = append(diagnostics, cleanupDiagnostic("lease_unlock_error", err))
		}
	}
	session.Status = finalStatus
	session.FinishedAt = m.clock()
	if err := m.saveSession(*session); err != nil {
		diagnostics = append(diagnostics, cleanupDiagnostic("session_save_error", err))
	}
	cleanupEvent := map[string]any{
		"at": m.clock().Format(time.RFC3339Nano), "kind": kind, "server_stopped": serverStopped,
		"driver_pid": session.DriverPID, "server_pid": session.ServerPID, "diagnostics": diagnostics,
	}
	sidecarErr := appendJSONLine(filepath.Join(session.ArtifactRoot, "host-cleanup.jsonl"), cleanupEvent)
	if sidecarErr != nil {
		diagnostics = append(diagnostics, cleanupDiagnostic("cleanup_sidecar_error", sidecarErr))
	}
	if err := m.patchCleanupIndex(*session, serverStopped, diagnostics, cleanupEvent); err != nil {
		fallback := map[string]any{
			"at": m.clock().Format(time.RFC3339Nano), "kind": kind,
			"diagnostics": []map[string]any{cleanupDiagnostic("cleanup_index_error", err)},
		}
		_ = appendJSONLine(filepath.Join(session.ArtifactRoot, "host-cleanup.jsonl"), fallback)
	}
}

func firstPresent(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func boolMetadata(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := values[key].(bool); ok && value {
			return true
		}
	}
	return false
}

func (m *PlaytestManager) patchCleanupIndex(session PlaytestSession, serverStopped bool, diagnostics []map[string]any, event map[string]any) error {
	index, err := loadOrCreatePlaytestIndex(session)
	if err != nil {
		return err
	}
	cleanup, _ := index["cleanup"].(map[string]any)
	if cleanup == nil {
		cleanup = map[string]any{}
	}
	cleanup["dev_server_reused"] = session.ServerReused
	cleanup["dev_server_stopped"] = serverStopped
	cleanup["dev_server_pid"] = session.ServerPID
	cleanup["driver_stopped"] = !processGroupAlive(session.DriverPID)
	index["cleanup"] = cleanup
	index["status"] = session.Status
	index["finished_at"] = session.FinishedAt
	appendIndexItems(index, "discrepancies", diagnostics...)
	appendIndexItems(index, "timeline", event)
	appendArtifact(index, "host-cleanup.jsonl")
	if err := appendJSONLine(filepath.Join(session.ArtifactRoot, "timeline.jsonl"), event); err != nil {
		return fmt.Errorf("appending host cleanup timeline: %w", err)
	}
	if err := refreshArtifactIndex(index, session.ArtifactRoot); err != nil {
		return fmt.Errorf("refreshing cleanup artifacts: %w", err)
	}
	appendArtifact(index, relativeArtifactPath(session.ArtifactRoot, session.IndexPath))
	return writePlaytestIndex(session.IndexPath, index)
}

func (m *PlaytestManager) persistManagerCallFailure(session PlaytestSession, request map[string]any, result map[string]any, diagnostic map[string]any) error {
	requestEntry := map[string]any{"at": m.clock().Format(time.RFC3339Nano), "request": request, "source": "manager_fallback"}
	if err := appendJSONLine(filepath.Join(session.ArtifactRoot, "requests.jsonl"), requestEntry); err != nil {
		return fmt.Errorf("persisting failed driver request: %w", err)
	}
	index, err := loadOrCreatePlaytestIndex(session)
	if err != nil {
		return err
	}
	timelineItems, _ := index["timeline"].([]any)
	event := map[string]any{
		"offset": len(timelineItems), "at": m.clock().Format(time.RFC3339Nano), "kind": "manager_call_failure",
		"request": request, "discrepancies": []map[string]any{diagnostic}, "result": result,
	}
	if err := appendJSONLine(filepath.Join(session.ArtifactRoot, "timeline.jsonl"), event); err != nil {
		return fmt.Errorf("persisting failed driver timeline: %w", err)
	}
	appendIndexItems(index, "timeline", event)
	appendIndexItems(index, "discrepancies", diagnostic)
	appendArtifact(index, "requests.jsonl")
	appendArtifact(index, "timeline.jsonl")
	if err := refreshArtifactIndex(index, session.ArtifactRoot); err != nil {
		return fmt.Errorf("refreshing failed-call artifacts: %w", err)
	}
	appendArtifact(index, relativeArtifactPath(session.ArtifactRoot, session.IndexPath))
	return writePlaytestIndex(session.IndexPath, index)
}

func loadOrCreatePlaytestIndex(session PlaytestSession) (map[string]any, error) {
	data, err := os.ReadFile(session.IndexPath)
	if err == nil {
		var index map[string]any
		if err := json.Unmarshal(data, &index); err != nil {
			return nil, fmt.Errorf("parsing playtest index: %w", err)
		}
		return index, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading playtest index: %w", err)
	}
	return map[string]any{
		"schema_version": PlaytestSchemaVersion, "session_id": session.SessionID, "project": session.Project,
		"repository": session.RepoRoot, "scenario": session.Scenario, "status": session.Status,
		"started_at": session.StartedAt, "timeline": []any{}, "discrepancies": []any{}, "artifacts": []any{},
		"cleanup": map[string]any{"browser_closed": false, "driver_stopped": !processAlive(session.DriverPID)},
	}, nil
}

func appendIndexItems(index map[string]any, key string, items ...map[string]any) {
	existing, _ := index[key].([]any)
	for _, item := range items {
		existing = append(existing, item)
	}
	index[key] = existing
}

func appendArtifact(index map[string]any, artifact string) {
	if artifact == "" {
		return
	}
	existing, _ := index["artifacts"].([]any)
	for _, value := range existing {
		if value == artifact {
			return
		}
	}
	index["artifacts"] = append(existing, artifact)
}

func relativeArtifactPath(artifactRoot string, path string) string {
	relative, err := filepath.Rel(artifactRoot, path)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return ""
	}
	return filepath.ToSlash(relative)
}

func refreshArtifactIndex(index map[string]any, artifactRoot string) error {
	artifacts := []string{}
	err := filepath.WalkDir(artifactRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(artifactRoot, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(filepath.Base(relative), ".playtest-index-") {
			return nil
		}
		artifacts = append(artifacts, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(artifacts)
	values := make([]any, len(artifacts))
	for index := range artifacts {
		values[index] = artifacts[index]
	}
	index["artifacts"] = values
	return nil
}

func appendJSONLine(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}

func writePlaytestIndex(path string, index map[string]any) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".playtest-index-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func cleanupDiagnostic(code string, err error) map[string]any {
	return map[string]any{"code": code, "error": err.Error(), "continued": true}
}

func (m *PlaytestManager) cleanupStartedServer(registry *LeaseRegistry, leases []LeaseRecord, sessionID string, server preparedServer) {
	if !server.reused && server.process != nil {
		_ = stopManagedProcess(server.process, m.cfg.Timeouts.ShutdownTimeout)
		_ = registry.Save(removeLease(leases, sessionID))
	}
}

func (m *PlaytestManager) sessionPath(sessionID string) string {
	return filepath.Join(m.cfg.StateDir, "playtest-sessions", sessionID+".json")
}

func (m *PlaytestManager) saveSession(session PlaytestSession) error {
	if err := os.MkdirAll(filepath.Dir(m.sessionPath(session.SessionID)), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.sessionPath(session.SessionID), append(data, '\n'), 0o600)
}

func reservePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserving playtest driver port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func ParseJSONRequest(raw string) (map[string]any, error) {
	request := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return request, nil
	}
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		return nil, fmt.Errorf("parsing request JSON: %w", err)
	}
	return request, nil
}

func FormatPlaytestResult(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func parseInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}
