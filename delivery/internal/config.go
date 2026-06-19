package delivery

import (
	"errors"
	"fmt"
	"os"
	"time"

	sharedconfig "den-services/shared/config"
	"gopkg.in/yaml.v3"
)

type Config struct {
	BindAddr            string
	DatabaseURL         string
	RuntimeServiceURL   string
	RuntimeServiceToken string
	ServiceToken        string
	DefaultTTL          time.Duration
	MaxTTL              time.Duration
	PendingTTL          time.Duration
	RunningTTL          time.Duration
	SweepInterval       time.Duration
	RuntimeHTTP         RuntimeHTTPConfig
	HTTP                HTTPConfig
}

type RuntimeHTTPConfig struct {
	Timeout time.Duration
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
}

type configFile struct {
	BindAddr          string             `yaml:"bind_addr"`
	DatabaseURL       string             `yaml:"database_url"`
	RuntimeServiceURL string             `yaml:"runtime_service_url"`
	Delivery          deliveryConfigFile `yaml:"delivery"`
	Reaper            reaperConfigFile   `yaml:"reaper"`
	RuntimeHTTP       runtimeHTTPFile    `yaml:"runtime_http"`
	HTTP              httpConfigFile     `yaml:"http"`
}

type deliveryConfigFile struct {
	DefaultTTL string `yaml:"default_ttl"`
	MaxTTL     string `yaml:"max_ttl"`
}

type reaperConfigFile struct {
	SweepInterval string `yaml:"sweep_interval"`
	PendingTTL    string `yaml:"pending_ttl"`
	RunningTTL    string `yaml:"running_ttl"`
}

type runtimeHTTPFile struct {
	Timeout string `yaml:"timeout"`
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
		return nil, fmt.Errorf("reading delivery config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing delivery config %s: %w", path, err)
	}
	databaseURL, err := values.Expand(file.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("expanding database_url: %w", err)
	}
	defaultTTL, err := parseRequiredDuration("delivery.default_ttl", file.Delivery.DefaultTTL)
	if err != nil {
		return nil, err
	}
	maxTTL, err := parseRequiredDuration("delivery.max_ttl", file.Delivery.MaxTTL)
	if err != nil {
		return nil, err
	}
	sweepInterval, err := parseRequiredDuration("reaper.sweep_interval", file.Reaper.SweepInterval)
	if err != nil {
		return nil, err
	}
	pendingTTL, err := parseRequiredDuration("reaper.pending_ttl", file.Reaper.PendingTTL)
	if err != nil {
		return nil, err
	}
	runningTTL, err := parseRequiredDuration("reaper.running_ttl", file.Reaper.RunningTTL)
	if err != nil {
		return nil, err
	}
	runtimeHTTP, err := file.RuntimeHTTP.toConfig()
	if err != nil {
		return nil, err
	}
	httpConfig, err := file.HTTP.toConfig()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		BindAddr:            file.BindAddr,
		DatabaseURL:         databaseURL,
		RuntimeServiceURL:   file.RuntimeServiceURL,
		RuntimeServiceToken: os.Getenv("DEN_DELIVERY_RUNTIME_SERVICE_TOKEN"),
		ServiceToken:        os.Getenv("DEN_DELIVERY_SERVICE_TOKEN"),
		DefaultTTL:          defaultTTL,
		MaxTTL:              maxTTL,
		PendingTTL:          pendingTTL,
		RunningTTL:          runningTTL,
		SweepInterval:       sweepInterval,
		RuntimeHTTP:         runtimeHTTP,
		HTTP:                httpConfig,
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
	if c.RuntimeServiceURL == "" {
		return errors.New("runtime_service_url is required")
	}
	if c.RuntimeServiceToken == "" {
		return ErrMissingRuntimeAuth
	}
	if c.DefaultTTL <= 0 || c.MaxTTL <= 0 || c.PendingTTL <= 0 || c.RunningTTL <= 0 || c.SweepInterval <= 0 {
		return errors.New("delivery ttl values must be positive")
	}
	if c.DefaultTTL > c.MaxTTL {
		return errors.New("delivery default ttl must not exceed max ttl")
	}
	if c.RuntimeHTTP.Timeout <= 0 {
		return errors.New("runtime_http timeout must be positive")
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		return errors.New("http read header timeout must be positive")
	}
	return nil
}

func (c runtimeHTTPFile) toConfig() (RuntimeHTTPConfig, error) {
	timeout, err := parseRequiredDuration("runtime_http.timeout", c.Timeout)
	if err != nil {
		return RuntimeHTTPConfig{}, err
	}
	return RuntimeHTTPConfig{Timeout: timeout}, nil
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
	path := os.Getenv("DELIVERY_CONFIG_PATH")
	if path == "" {
		return "config/config.yaml"
	}
	return path
}
