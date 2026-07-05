package serve

import (
	"fmt"
	"os"
	"strings"
	"time"

	devserver "den-services/devserver-broker"

	"gopkg.in/yaml.v3"
)

func DefaultConfig() (devserver.ManagerConfig, error) {
	return devserver.NormalizeConfig(devserver.ManagerConfig{
		StateDir:    "~/.cache/den-serve/state",
		SessionRoot: "~/.cache/den-serve/sessions",
		BindHost:    devserver.DefaultBindHost,
		ProbeHost:   devserver.DefaultProbeHost,
		PublicHost:  devserver.PublicHostAuto,
		PortRange: devserver.PortRange{
			Start: 37300,
			End:   37450,
		},
		Timeouts: devserver.TimeoutConfig{
			LockTimeout:     10 * time.Second,
			StartupTimeout:  45 * time.Second,
			HealthTimeout:   2 * time.Second,
			HealthInterval:  250 * time.Millisecond,
			ShutdownTimeout: 5 * time.Second,
		},
	})
}

type configFile struct {
	StateDir    string        `yaml:"state_dir"`
	SessionRoot string        `yaml:"session_root"`
	BindHost    string        `yaml:"bind_host"`
	ProbeHost   string        `yaml:"probe_host"`
	PublicHost  string        `yaml:"public_host"`
	PortRange   portRangeFile `yaml:"port_range"`
	Timeouts    timeoutFile   `yaml:"timeouts"`
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
}

func LoadConfigFromPath(path string) (devserver.ManagerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return devserver.ManagerConfig{}, fmt.Errorf("reading den-serve config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return devserver.ManagerConfig{}, fmt.Errorf("parsing den-serve config %s: %w", path, err)
	}
	cfg, err := file.toConfig()
	if err != nil {
		return devserver.ManagerConfig{}, err
	}
	return devserver.NormalizeConfig(cfg)
}

func (f configFile) toConfig() (devserver.ManagerConfig, error) {
	timeouts, err := f.Timeouts.toConfig()
	if err != nil {
		return devserver.ManagerConfig{}, err
	}
	return devserver.ManagerConfig{
		StateDir:    f.StateDir,
		SessionRoot: f.SessionRoot,
		BindHost:    valueOrDefault(f.BindHost, devserver.DefaultBindHost),
		ProbeHost:   valueOrDefault(f.ProbeHost, devserver.DefaultProbeHost),
		PublicHost:  valueOrDefault(f.PublicHost, devserver.PublicHostAuto),
		PortRange: devserver.PortRange{
			Start: f.PortRange.Start,
			End:   f.PortRange.End,
		},
		Timeouts: timeouts,
	}, nil
}

func (f timeoutFile) toConfig() (devserver.TimeoutConfig, error) {
	lockTimeout, err := parseRequiredDuration("timeouts.lock_timeout", f.LockTimeout)
	if err != nil {
		return devserver.TimeoutConfig{}, err
	}
	startupTimeout, err := parseRequiredDuration("timeouts.startup_timeout", f.StartupTimeout)
	if err != nil {
		return devserver.TimeoutConfig{}, err
	}
	healthTimeout, err := parseRequiredDuration("timeouts.health_timeout", f.HealthTimeout)
	if err != nil {
		return devserver.TimeoutConfig{}, err
	}
	healthInterval, err := parseRequiredDuration("timeouts.health_interval", f.HealthInterval)
	if err != nil {
		return devserver.TimeoutConfig{}, err
	}
	shutdownTimeout, err := parseRequiredDuration("timeouts.shutdown_timeout", f.ShutdownTimeout)
	if err != nil {
		return devserver.TimeoutConfig{}, err
	}
	return devserver.TimeoutConfig{
		LockTimeout:     lockTimeout,
		StartupTimeout:  startupTimeout,
		HealthTimeout:   healthTimeout,
		HealthInterval:  healthInterval,
		ShutdownTimeout: shutdownTimeout,
	}, nil
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

func valueOrDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
