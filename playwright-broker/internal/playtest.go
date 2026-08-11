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
		"metadata":     options.Metadata,
		"revision":     readRevision(repoRoot),
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
		result = map[string]any{
			"ok":            false,
			"continued":     processAlive(session.DriverPID),
			"session_id":    session.SessionID,
			"index_path":    session.IndexPath,
			"result":        map[string]any{"partial": true, "error": callErr.Error()},
			"discrepancies": []map[string]any{{"code": "driver_call_error", "error": callErr.Error()}},
		}
	}
	if kind == "finish" || kind == "cancel" {
		m.finishHostCleanup(&session, kind)
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

func (m *PlaytestManager) finishHostCleanup(session *PlaytestSession, kind string) {
	driverDeadline := m.clock().Add(m.cfg.Timeouts.ShutdownTimeout)
	for processAlive(session.DriverPID) && m.clock().Before(driverDeadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if processAlive(session.DriverPID) {
		_ = stopProcessGroup(session.DriverPID, m.cfg.Timeouts.ShutdownTimeout)
	}
	serverStopped := session.ServerReused || session.ServerPID <= 0
	if !session.ServerReused && session.ServerPID > 0 {
		serverStopped = stopProcessGroup(session.ServerPID, m.cfg.Timeouts.ShutdownTimeout) == nil
	}
	registry := NewLeaseRegistry(m.cfg.StateDir)
	if registry.Lock(context.Background(), m.cfg.Timeouts.LockTimeout) == nil {
		if leases, err := registry.Load(); err == nil {
			_ = registry.Save(removeLease(leases, session.SessionID))
		}
		_ = registry.Unlock()
	}
	session.Status = kind
	session.FinishedAt = m.clock()
	_ = m.saveSession(*session)
	m.patchCleanupIndex(*session, serverStopped)
}

func (m *PlaytestManager) patchCleanupIndex(session PlaytestSession, serverStopped bool) {
	data, err := os.ReadFile(session.IndexPath)
	if err != nil {
		return
	}
	var index map[string]any
	if json.Unmarshal(data, &index) != nil {
		return
	}
	cleanup, _ := index["cleanup"].(map[string]any)
	if cleanup == nil {
		cleanup = map[string]any{}
	}
	cleanup["dev_server_reused"] = session.ServerReused
	cleanup["dev_server_stopped"] = serverStopped
	cleanup["dev_server_pid"] = session.ServerPID
	index["cleanup"] = cleanup
	encoded, err := json.MarshalIndent(index, "", "  ")
	if err == nil {
		_ = os.WriteFile(session.IndexPath, append(encoded, '\n'), 0o600)
	}
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

func readRevision(repoRoot string) map[string]any {
	revision := map[string]any{}
	if output, err := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD").Output(); err == nil {
		revision["commit_sha"] = strings.TrimSpace(string(output))
	}
	if output, err := exec.Command("git", "-C", repoRoot, "status", "--short").Output(); err == nil {
		status := strings.TrimSpace(string(output))
		revision["dirty"] = status != ""
		revision["dirty_status"] = status
	}
	if output, err := exec.Command("git", "-C", repoRoot, "remote", "get-url", "origin").Output(); err == nil {
		revision["origin"] = strings.TrimSpace(string(output))
	}
	return revision
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
