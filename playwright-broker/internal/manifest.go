package broker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type manifestFile struct {
	Project string            `json:"project"`
	Serve   serveManifestFile `json:"serve"`
	Tests   testManifestFile  `json:"tests"`
}

type serveManifestFile struct {
	Command        string            `json:"command"`
	Host           string            `json:"host"`
	PreferredPort  int               `json:"preferredPort"`
	PortRange      *portRangeFile    `json:"portRange"`
	HealthURL      string            `json:"healthUrl"`
	ReadyText      string            `json:"readyText"`
	IdentityHeader string            `json:"identityHeader"`
	ReusePolicy    string            `json:"reusePolicy"`
	StartupTimeout string            `json:"startupTimeout"`
	HealthInterval string            `json:"healthInterval"`
	Environment    map[string]string `json:"env"`
}

type testManifestFile struct {
	Command        string            `json:"command"`
	ConfigPath     string            `json:"config"`
	DefaultArgs    []string          `json:"defaultArgs"`
	ArtifactPolicy string            `json:"artifactPolicy"`
	OutputDir      string            `json:"outputDir"`
	Environment    map[string]string `json:"env"`
}

func LoadManifest(path string, repoRoot string, cfg *Config) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", path, err)
	}
	var file manifestFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", path, err)
	}
	manifest, err := file.toManifest(repoRoot, cfg)
	if err != nil {
		return nil, err
	}
	if err := manifest.validate(); err != nil {
		return nil, err
	}
	return manifest, nil
}

func FindManifest(repoRoot string, explicitPath string) (string, error) {
	if strings.TrimSpace(explicitPath) != "" {
		return filepath.Abs(explicitPath)
	}
	candidates := []string{
		DefaultManifestName,
		".playwright-service.json",
		"den-playwright.json",
	}
	for _, name := range candidates {
		path := filepath.Join(repoRoot, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%w: no manifest found under %s", ErrInvalidManifest, repoRoot)
}

func (f manifestFile) toManifest(repoRoot string, cfg *Config) (*Manifest, error) {
	serve, err := f.Serve.toManifest(cfg)
	if err != nil {
		return nil, err
	}
	tests := f.Tests.toManifest()
	return &Manifest{
		Project:  strings.TrimSpace(f.Project),
		RepoRoot: filepath.Clean(repoRoot),
		Serve:    serve,
		Tests:    tests,
	}, nil
}

func (f serveManifestFile) toManifest(cfg *Config) (ServeManifest, error) {
	startupTimeout := cfg.Timeouts.StartupTimeout
	if strings.TrimSpace(f.StartupTimeout) != "" {
		parsed, err := time.ParseDuration(f.StartupTimeout)
		if err != nil {
			return ServeManifest{}, fmt.Errorf("%w: parsing serve.startupTimeout: %w", ErrInvalidManifest, err)
		}
		startupTimeout = parsed
	}
	healthInterval := cfg.Timeouts.HealthInterval
	if strings.TrimSpace(f.HealthInterval) != "" {
		parsed, err := time.ParseDuration(f.HealthInterval)
		if err != nil {
			return ServeManifest{}, fmt.Errorf("%w: parsing serve.healthInterval: %w", ErrInvalidManifest, err)
		}
		healthInterval = parsed
	}
	reusePolicy := ReusePolicy(valueOrDefault(f.ReusePolicy, string(ReusePolicyBrokerOwned)))
	portRange := (*PortRange)(nil)
	if f.PortRange != nil {
		portRange = &PortRange{Start: f.PortRange.Start, End: f.PortRange.End}
	}
	return ServeManifest{
		Command:        strings.TrimSpace(f.Command),
		Host:           valueOrDefault(f.Host, cfg.Host),
		PreferredPort:  f.PreferredPort,
		PortRange:      portRange,
		HealthPath:     valueOrDefault(f.HealthURL, "/"),
		ReadyText:      f.ReadyText,
		IdentityHeader: f.IdentityHeader,
		ReusePolicy:    reusePolicy,
		StartupTimeout: startupTimeout,
		HealthInterval: healthInterval,
		Environment:    copyMap(f.Environment),
	}, nil
}

func (f testManifestFile) toManifest() TestManifest {
	policy := ArtifactPolicy(valueOrDefault(f.ArtifactPolicy, string(ArtifactPolicyStandard)))
	return TestManifest{
		Command:        valueOrDefault(f.Command, "npx playwright test"),
		ConfigPath:     strings.TrimSpace(f.ConfigPath),
		DefaultArgs:    append([]string(nil), f.DefaultArgs...),
		ArtifactPolicy: policy,
		OutputDir:      strings.TrimSpace(f.OutputDir),
		Environment:    copyMap(f.Environment),
	}
}

func (m *Manifest) validate() error {
	if strings.TrimSpace(m.Project) == "" {
		return fmt.Errorf("%w: project is required", ErrInvalidManifest)
	}
	if strings.TrimSpace(m.RepoRoot) == "" {
		return fmt.Errorf("%w: repo root is required", ErrInvalidManifest)
	}
	if strings.TrimSpace(m.Serve.Command) == "" {
		return fmt.Errorf("%w: serve.command is required", ErrInvalidManifest)
	}
	if m.Serve.PreferredPort < 0 {
		return fmt.Errorf("%w: serve.preferredPort cannot be negative", ErrInvalidManifest)
	}
	if m.Serve.PortRange != nil && (m.Serve.PortRange.Start <= 0 || m.Serve.PortRange.End < m.Serve.PortRange.Start) {
		return fmt.Errorf("%w: serve.portRange must be positive and ordered", ErrInvalidManifest)
	}
	if !m.Serve.ReusePolicy.IsValid() {
		return fmt.Errorf("%w: serve.reusePolicy is invalid", ErrInvalidManifest)
	}
	if strings.TrimSpace(m.Serve.ReadyText) == "" && strings.TrimSpace(m.Serve.IdentityHeader) == "" {
		return fmt.Errorf("%w: serve.readyText or serve.identityHeader is required", ErrInvalidManifest)
	}
	if m.Serve.StartupTimeout <= 0 || m.Serve.HealthInterval <= 0 {
		return fmt.Errorf("%w: serve timeouts must be positive", ErrInvalidManifest)
	}
	if strings.TrimSpace(m.Tests.Command) == "" {
		return fmt.Errorf("%w: tests.command is required", ErrInvalidManifest)
	}
	switch m.Tests.ArtifactPolicy {
	case ArtifactPolicyStandard, ArtifactPolicyLiveUI:
	default:
		return fmt.Errorf("%w: tests.artifactPolicy is invalid", ErrInvalidManifest)
	}
	return nil
}

func (m *Manifest) PortRange(cfg *Config) PortRange {
	if m.Serve.PortRange != nil {
		return *m.Serve.PortRange
	}
	return cfg.PortRange
}

func (m *Manifest) HealthURL(port int) string {
	path := m.Serve.HealthPath
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("http://%s:%d%s", m.Serve.Host, port, path)
}

func copyMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	target := make(map[string]string, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}
