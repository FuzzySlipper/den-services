package broker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type configFile struct {
	StateDir     string        `yaml:"state_dir"`
	ArtifactRoot string        `yaml:"artifact_root"`
	Host         string        `yaml:"host"`
	PortRange    portRangeFile `yaml:"port_range"`
	Timeouts     timeoutFile   `yaml:"timeouts"`
}

type portRangeFile struct {
	Start int `yaml:"start"`
	End   int `yaml:"end"`
}

type timeoutFile struct {
	LockTimeout     string `yaml:"lock_timeout"`
	StartupTimeout  string `yaml:"startup_timeout"`
	HealthTimeout   string `yaml:"health_timeout"`
	HealthInterval  string `yaml:"health_interval"`
	ShutdownTimeout string `yaml:"shutdown_timeout"`
	RunTimeout      string `yaml:"run_timeout"`
}

func LoadConfigFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading playwright broker config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing playwright broker config %s: %w", path, err)
	}
	cfg, err := file.toConfig()
	if err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (f configFile) toConfig() (*Config, error) {
	timeouts, err := f.Timeouts.toConfig()
	if err != nil {
		return nil, err
	}
	stateDir, err := cleanConfiguredPath("state_dir", f.StateDir)
	if err != nil {
		return nil, err
	}
	artifactRoot, err := cleanConfiguredPath("artifact_root", f.ArtifactRoot)
	if err != nil {
		return nil, err
	}
	return &Config{
		StateDir:     stateDir,
		ArtifactRoot: artifactRoot,
		Host:         valueOrDefault(f.Host, DefaultHost),
		PortRange: PortRange{
			Start: f.PortRange.Start,
			End:   f.PortRange.End,
		},
		Timeouts: timeouts,
	}, nil
}

func (f timeoutFile) toConfig() (TimeoutConfig, error) {
	lockTimeout, err := parseRequiredDuration("timeouts.lock_timeout", f.LockTimeout)
	if err != nil {
		return TimeoutConfig{}, err
	}
	startupTimeout, err := parseRequiredDuration("timeouts.startup_timeout", f.StartupTimeout)
	if err != nil {
		return TimeoutConfig{}, err
	}
	healthTimeout, err := parseRequiredDuration("timeouts.health_timeout", f.HealthTimeout)
	if err != nil {
		return TimeoutConfig{}, err
	}
	healthInterval, err := parseRequiredDuration("timeouts.health_interval", f.HealthInterval)
	if err != nil {
		return TimeoutConfig{}, err
	}
	shutdownTimeout, err := parseRequiredDuration("timeouts.shutdown_timeout", f.ShutdownTimeout)
	if err != nil {
		return TimeoutConfig{}, err
	}
	runTimeout, err := parseRequiredDuration("timeouts.run_timeout", f.RunTimeout)
	if err != nil {
		return TimeoutConfig{}, err
	}
	return TimeoutConfig{
		LockTimeout:     lockTimeout,
		StartupTimeout:  startupTimeout,
		HealthTimeout:   healthTimeout,
		HealthInterval:  healthInterval,
		ShutdownTimeout: shutdownTimeout,
		RunTimeout:      runTimeout,
	}, nil
}

func (c *Config) validate() error {
	if c.StateDir == "" {
		return errors.New("state_dir is required")
	}
	if c.ArtifactRoot == "" {
		return errors.New("artifact_root is required")
	}
	if c.Host == "" {
		return errors.New("host is required")
	}
	if c.PortRange.Start <= 0 || c.PortRange.End < c.PortRange.Start {
		return errors.New("port_range must be positive and ordered")
	}
	if c.Timeouts.LockTimeout <= 0 ||
		c.Timeouts.StartupTimeout <= 0 ||
		c.Timeouts.HealthTimeout <= 0 ||
		c.Timeouts.HealthInterval <= 0 ||
		c.Timeouts.ShutdownTimeout <= 0 ||
		c.Timeouts.RunTimeout <= 0 {
		return errors.New("timeouts must be positive")
	}
	return nil
}

func parseRequiredDuration(name string, raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", name, err)
	}
	return parsed, nil
}

func cleanConfiguredPath(name string, raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory for %s: %w", name, err)
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolving %s: %w", name, err)
		}
		path = abs
	}
	return filepath.Clean(path), nil
}

func valueOrDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
