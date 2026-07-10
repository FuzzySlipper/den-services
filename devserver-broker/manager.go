package devserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Manager struct {
	cfg        ManagerConfig
	httpClient *http.Client
	clock      func() time.Time
}

func NewManager(cfg ManagerConfig) (*Manager, error) {
	normalized, err := NormalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Manager{
		cfg: normalized,
		httpClient: &http.Client{
			Timeout: normalized.Timeouts.HealthTimeout,
		},
		clock: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (m *Manager) Up(ctx context.Context, options UpOptions) (UpResult, error) {
	if strings.TrimSpace(options.Project) == "" {
		return UpResult{}, errors.New("project is required")
	}
	repoRoot, err := resolveRepoRoot(options.RepoRoot)
	if err != nil {
		return UpResult{}, err
	}
	manifestPath, err := FindManifest(repoRoot, options.ManifestPath)
	if err != nil {
		return UpResult{}, err
	}
	manifest, err := LoadServeManifest(manifestPath, repoRoot, m.cfg)
	if err != nil {
		return UpResult{}, err
	}
	if manifest.Project != options.Project {
		return UpResult{}, fmt.Errorf("%w: requested project %q but manifest project is %q", ErrInvalidManifest, options.Project, manifest.Project)
	}
	if strings.TrimSpace(options.PublicHostOverride) != "" {
		manifest.PublicHost = options.PublicHostOverride
	}
	sessionKey := SessionKey(manifest.Project, repoRoot)
	registry := NewLeaseRegistry(m.cfg.StateDir)
	if err := registry.Lock(ctx, m.cfg.Timeouts.LockTimeout); err != nil {
		return UpResult{}, err
	}
	defer registry.Unlock()
	leases, err := registry.Load()
	if err != nil {
		return UpResult{}, err
	}
	leases = pruneDeadLeases(leases)
	store := NewSessionStore(m.cfg.SessionRoot)
	if existing, ok := m.reusableCurrentSession(ctx, store, manifest, sessionKey); ok {
		existing.LastCheckedAt = m.clock()
		existing.Status = "running"
		if err := store.WriteCurrent(existing); err != nil {
			return UpResult{}, err
		}
		return UpResult{Session: existing, Reused: true}, nil
	}
	port, reused, reuseSource, health, err := m.selectPortOrReuse(ctx, manifest, leases)
	if err != nil {
		_ = registry.Save(leases)
		return UpResult{}, err
	}
	now := m.clock()
	sessionID := newSessionID(manifest.Project, now)
	sessionDir := store.SessionDir(sessionKey, sessionID)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return UpResult{}, fmt.Errorf("creating session dir: %w", err)
	}
	publicHost := ResolvePublicHost(manifest.PublicHost)
	local := localURL(manifest.ProbeHost, port)
	lan := publicURL(publicHost, port)
	values := templateContext{
		project:    manifest.Project,
		repoRoot:   manifest.RepoRoot,
		bindHost:   manifest.BindHost,
		probeHost:  manifest.ProbeHost,
		port:       port,
		localURL:   local,
		publicURL:  lan,
		sessionDir: sessionDir,
	}
	command := renderTemplate(manifest.Command, values)
	session := SessionState{
		SchemaVersion: SessionSchemaV0,
		SessionID:     sessionID,
		SessionKey:    sessionKey,
		Project:       manifest.Project,
		Target:        manifest.Target,
		RepoRoot:      manifest.RepoRoot,
		ManifestPath:  manifest.ManifestPath,
		Command:       command,
		BindHost:      manifest.BindHost,
		ProbeHost:     manifest.ProbeHost,
		PublicHost:    publicHost,
		Port:          port,
		LocalURL:      local,
		LANURL:        lan,
		HealthURL:     healthURL(manifest.ProbeHost, port, manifest.HealthPath),
		ReuseSource:   reuseSource,
		Status:        "running",
		Health:        health,
		StartedAt:     now,
		LastCheckedAt: now,
		StdoutLog:     filepath.Join(sessionDir, "server.stdout.log"),
		StderrLog:     filepath.Join(sessionDir, "server.stderr.log"),
		StatePath:     store.CurrentPath(sessionKey),
		SessionDir:    sessionDir,
	}
	hash, err := ManifestHash(manifest.ManifestPath)
	if err != nil {
		return UpResult{}, err
	}
	session.ManifestHash = hash
	if reused {
		if lease := findLeaseForPort(leases, manifest.ProbeHost, port); lease != nil {
			session.PID = lease.PID
			session.Ownership = "broker_owned"
		} else {
			session.Ownership = "unowned"
		}
		if err := store.WriteCurrent(session); err != nil {
			return UpResult{}, err
		}
		return UpResult{Session: session, Reused: true}, nil
	}
	env := map[string]string{
		"HOST":                  manifest.BindHost,
		"PORT":                  strconv.Itoa(port),
		"BASE_URL":              local,
		"DEN_SERVE_LOCAL_URL":   local,
		"DEN_SERVE_PUBLIC_URL":  lan,
		"DEN_SERVE_SESSION_DIR": sessionDir,
	}
	for key, value := range manifest.Environment {
		env[key] = renderTemplate(value, values)
	}
	process, err := startManagedProcess(command, manifest.RepoRoot, env, session.StdoutLog, session.StderrLog)
	if err != nil {
		_ = registry.Save(leases)
		return UpResult{}, err
	}
	session.PID = process.PID
	session.Ownership = "broker_owned"
	session.ReuseSource = "started"
	health, err = waitForHealth(ctx, m.httpClient, manifest, port)
	session.Health = health
	if err != nil {
		_ = StopProcessGroup(process.PID, m.cfg.Timeouts.ShutdownTimeout)
		session.Status = "failed"
		_ = store.WriteCurrent(session)
		_ = registry.Save(leases)
		return UpResult{Session: session}, err
	}
	lease := LeaseRecord{
		SessionID:  session.SessionID,
		SessionKey: session.SessionKey,
		Project:    session.Project,
		Target:     session.Target,
		RepoRoot:   session.RepoRoot,
		BindHost:   session.BindHost,
		ProbeHost:  session.ProbeHost,
		Port:       session.Port,
		LocalURL:   session.LocalURL,
		LANURL:     session.LANURL,
		Command:    session.Command,
		PID:        session.PID,
		StartedAt:  session.StartedAt,
	}
	leases = upsertLease(leases, lease)
	if err := registry.Save(leases); err != nil {
		_ = StopProcessGroup(process.PID, m.cfg.Timeouts.ShutdownTimeout)
		return UpResult{}, err
	}
	if err := store.WriteCurrent(session); err != nil {
		return UpResult{}, err
	}
	return UpResult{Session: session, Started: true}, nil
}

func (m *Manager) Status(ctx context.Context, options StatusOptions) (SessionState, error) {
	store := NewSessionStore(m.cfg.SessionRoot)
	repoRoot := strings.TrimSpace(options.RepoRoot)
	if repoRoot != "" {
		resolved, err := resolveRepoRoot(repoRoot)
		if err != nil {
			return SessionState{}, err
		}
		repoRoot = resolved
	}
	session, err := store.FindCurrent(options.Project, repoRoot)
	if err != nil {
		return SessionState{}, err
	}
	return m.refreshSession(ctx, store, session)
}

func (m *Manager) List(ctx context.Context) ([]SessionState, error) {
	store := NewSessionStore(m.cfg.SessionRoot)
	sessions, err := store.List()
	if err != nil {
		return nil, err
	}
	refreshed := make([]SessionState, 0, len(sessions))
	for _, session := range sessions {
		current, err := m.refreshSession(ctx, store, session)
		if err != nil {
			refreshed = append(refreshed, session)
			continue
		}
		refreshed = append(refreshed, current)
	}
	return refreshed, nil
}

func (m *Manager) Stop(ctx context.Context, options StopOptions) (StopResult, error) {
	store := NewSessionStore(m.cfg.SessionRoot)
	repoRoot := strings.TrimSpace(options.RepoRoot)
	if repoRoot != "" {
		resolved, err := resolveRepoRoot(repoRoot)
		if err != nil {
			return StopResult{}, err
		}
		repoRoot = resolved
	}
	session, err := store.FindCurrent(options.Project, repoRoot)
	if err != nil {
		return StopResult{}, err
	}
	if session.Ownership != "broker_owned" || session.PID <= 0 {
		session.Status = "unowned"
		session.LastCheckedAt = m.clock()
		if err := store.WriteCurrent(session); err != nil {
			return StopResult{}, err
		}
		return StopResult{Session: session, Message: "session is not broker-owned; leaving process untouched"}, nil
	}
	if err := StopProcessGroup(session.PID, m.cfg.Timeouts.ShutdownTimeout); err != nil {
		return StopResult{}, err
	}
	registry := NewLeaseRegistry(m.cfg.StateDir)
	if err := registry.Lock(ctx, m.cfg.Timeouts.LockTimeout); err != nil {
		return StopResult{}, err
	}
	leases, loadErr := registry.Load()
	if loadErr == nil {
		_ = registry.Save(removeLease(leases, session.SessionKey))
	}
	_ = registry.Unlock()
	session.Status = "stopped"
	session.LastCheckedAt = m.clock()
	if err := store.WriteCurrent(session); err != nil {
		return StopResult{}, err
	}
	return StopResult{Session: session, Stopped: true, Message: "stopped broker-owned process group"}, nil
}

func (m *Manager) reusableCurrentSession(ctx context.Context, store *SessionStore, manifest *ServeManifest, sessionKey string) (SessionState, bool) {
	session, err := store.ReadCurrentByKey(sessionKey)
	if err != nil || session.PID <= 0 || session.Ownership != "broker_owned" || !processGroupAlive(session.PID) {
		return SessionState{}, false
	}
	health := checkHealth(ctx, m.httpClient, manifest, session.Port)
	if !health.Matched {
		return SessionState{}, false
	}
	session.Health = health
	return session, true
}

func (m *Manager) refreshSession(ctx context.Context, store *SessionStore, session SessionState) (SessionState, error) {
	manifest := &ServeManifest{
		Project:        session.Project,
		ProbeHost:      session.ProbeHost,
		HealthPath:     strings.TrimPrefix(strings.TrimPrefix(session.HealthURL, "http://"+session.ProbeHost+":"+strconv.Itoa(session.Port)), ""),
		ReadyText:      "",
		IdentityHeader: "",
	}
	if session.ManifestPath != "" {
		loaded, err := LoadServeManifest(session.ManifestPath, session.RepoRoot, m.cfg)
		if err == nil {
			manifest = loaded
		}
	}
	health := checkHealth(ctx, m.httpClient, manifest, session.Port)
	session.Health = health
	session.LastCheckedAt = m.clock()
	switch {
	case session.PID > 0 && !processGroupAlive(session.PID):
		session.Status = "stopped"
	case health.Matched:
		session.Status = "running"
	case session.PID == 0 && session.Ownership == "unowned":
		session.Status = "unowned"
	default:
		session.Status = "unhealthy"
	}
	if session.StatePath != "" {
		if err := store.WriteCurrent(session); err != nil {
			return SessionState{}, err
		}
	}
	return session, nil
}

func (m *Manager) selectPortOrReuse(ctx context.Context, manifest *ServeManifest, leases []LeaseRecord) (int, bool, string, HealthResult, error) {
	blocked := make(map[int]bool)
	for _, lease := range leases {
		if lease.ProbeHost == manifest.ProbeHost && processGroupAlive(lease.PID) {
			blocked[lease.Port] = true
		}
	}
	if manifest.PreferredPort > 0 {
		port := manifest.PreferredPort
		if portInUse(manifest.ProbeHost, port, m.cfg.Timeouts.HealthTimeout) {
			health := checkHealth(ctx, m.httpClient, manifest, port)
			lease := findLeaseForPort(leases, manifest.ProbeHost, port)
			if health.Matched && manifest.ReusePolicy != ReusePolicyNever {
				if lease != nil && processGroupAlive(lease.PID) && lease.Project == manifest.Project && lease.RepoRoot == manifest.RepoRoot {
					return port, true, "broker_owned", health, nil
				}
				if manifest.ReusePolicy == ReusePolicyExplicit {
					return port, true, "explicit", health, nil
				}
			}
			blocked[port] = true
		} else {
			return port, false, "", HealthResult{URL: healthURL(manifest.ProbeHost, port, manifest.HealthPath)}, nil
		}
	}
	portRange := manifest.EffectivePortRange(m.cfg)
	port, err := findFreePort(manifest.BindHost, portRange, blocked)
	if err != nil {
		return 0, false, "", HealthResult{}, err
	}
	return port, false, "", HealthResult{URL: healthURL(manifest.ProbeHost, port, manifest.HealthPath)}, nil
}
