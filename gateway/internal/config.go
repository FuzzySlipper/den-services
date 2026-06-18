package gateway

import (
	"errors"
	"fmt"
	"os"
	"time"

	sharedconfig "den-services/shared/config"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BindAddr          string
	RoutingConfigPath string
	ServiceToken      string
	HTTP              HTTPConfig
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
}

type configFile struct {
	BindAddr          string         `yaml:"bind_addr"`
	RoutingConfigPath string         `yaml:"routing_config_path"`
	HTTP              httpConfigFile `yaml:"http"`
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
		return nil, fmt.Errorf("reading gateway config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing gateway config %s: %w", path, err)
	}
	httpConfig, err := file.HTTP.toConfig()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		BindAddr:          file.BindAddr,
		RoutingConfigPath: file.RoutingConfigPath,
		ServiceToken:      values.String("DEN_GATEWAY_SERVICE_TOKEN", ""),
		HTTP:              httpConfig,
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
	if c.RoutingConfigPath == "" {
		return errors.New("routing_config_path is required")
	}
	if c.ServiceToken == "" {
		return errors.New("DEN_GATEWAY_SERVICE_TOKEN is required")
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		return errors.New("http read header timeout must be positive")
	}
	return nil
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
	path := os.Getenv("GATEWAY_CONFIG_PATH")
	if path == "" {
		return "config/config.yaml"
	}
	return path
}
