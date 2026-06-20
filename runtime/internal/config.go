package runtime

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	sharedconfig "den-services/shared/config"
)

type Config struct {
	BindAddr     string
	DatabaseURL  string
	ServiceToken string
	Heartbeat    HeartbeatConfig
	HTTP         HTTPConfig
}

type HeartbeatConfig struct {
	Interval       time.Duration
	StaleThreshold time.Duration
	DeadThreshold  time.Duration
	SweepInterval  time.Duration
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
}

type configFile struct {
	BindAddr    string              `yaml:"bind_addr"`
	DatabaseURL string              `yaml:"database_url"`
	Heartbeat   heartbeatConfigFile `yaml:"heartbeat"`
	HTTP        httpConfigFile      `yaml:"http"`
}

type heartbeatConfigFile struct {
	Interval       string `yaml:"interval"`
	StaleThreshold string `yaml:"stale_threshold"`
	DeadThreshold  string `yaml:"dead_threshold"`
	SweepInterval  string `yaml:"sweep_interval"`
}

type httpConfigFile struct {
	ReadHeaderTimeout string `yaml:"read_header_timeout"`
}

func LoadConfig() (*Config, error) {
	return LoadConfigFromPath(configPath())
}

func LoadConfigFromPath(path string) (*Config, error) {
	values, err := sharedconfig.Load()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading runtime config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing runtime config %s: %w", path, err)
	}
	databaseURL, err := values.Expand(file.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("expanding database_url: %w", err)
	}
	heartbeat, err := file.Heartbeat.toConfig()
	if err != nil {
		return nil, err
	}
	httpConfig, err := file.HTTP.toConfig()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		BindAddr:     file.BindAddr,
		DatabaseURL:  databaseURL,
		ServiceToken: os.Getenv("DEN_RUNTIME_SERVICE_TOKEN"),
		Heartbeat:    heartbeat,
		HTTP:         httpConfig,
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.BindAddr == "" {
		return errors.New("bind_addr is required")
	}
	if c.DatabaseURL == "" {
		return errors.New("database_url is required")
	}
	if c.Heartbeat.Interval <= 0 {
		return errors.New("heartbeat interval must be positive")
	}
	if c.Heartbeat.StaleThreshold <= c.Heartbeat.Interval {
		return errors.New("stale threshold must be greater than heartbeat interval")
	}
	if c.Heartbeat.DeadThreshold <= c.Heartbeat.StaleThreshold {
		return errors.New("dead threshold must be greater than stale threshold")
	}
	if c.Heartbeat.SweepInterval <= 0 {
		return errors.New("heartbeat sweep interval must be positive")
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		return errors.New("http read header timeout must be positive")
	}
	return nil
}

func (c heartbeatConfigFile) toConfig() (HeartbeatConfig, error) {
	interval, err := parseRequiredDuration("heartbeat.interval", c.Interval)
	if err != nil {
		return HeartbeatConfig{}, err
	}
	staleThreshold, err := parseRequiredDuration("heartbeat.stale_threshold", c.StaleThreshold)
	if err != nil {
		return HeartbeatConfig{}, err
	}
	deadThreshold, err := parseRequiredDuration("heartbeat.dead_threshold", c.DeadThreshold)
	if err != nil {
		return HeartbeatConfig{}, err
	}
	sweepInterval, err := parseRequiredDuration("heartbeat.sweep_interval", c.SweepInterval)
	if err != nil {
		return HeartbeatConfig{}, err
	}
	return HeartbeatConfig{
		Interval:       interval,
		StaleThreshold: staleThreshold,
		DeadThreshold:  deadThreshold,
		SweepInterval:  sweepInterval,
	}, nil
}

func (c httpConfigFile) toConfig() (HTTPConfig, error) {
	readHeaderTimeout, err := parseRequiredDuration("http.read_header_timeout", c.ReadHeaderTimeout)
	if err != nil {
		return HTTPConfig{}, err
	}
	return HTTPConfig{ReadHeaderTimeout: readHeaderTimeout}, nil
}

func parseRequiredDuration(name string, raw string) (time.Duration, error) {
	if raw == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", name, err)
	}
	return parsed, nil
}

func configPath() string {
	path := os.Getenv("RUNTIME_CONFIG_PATH")
	if path == "" {
		return "config/config.yaml"
	}
	return path
}

func (c *Config) String() string {
	return fmt.Sprintf("bind=%s stale=%s dead=%s", c.BindAddr, c.Heartbeat.StaleThreshold, c.Heartbeat.DeadThreshold)
}
