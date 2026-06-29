package projects

import (
	"errors"
	"fmt"
	"os"
	"time"

	sharedconfig "den-services/shared/config"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BindAddr     string
	DatabaseURL  string
	ServiceToken string
	HTTP         HTTPConfig
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
}

type configFile struct {
	BindAddr        string         `yaml:"bind_addr"`
	DatabaseURLEnv  string         `yaml:"database_url_env"`
	ServiceTokenEnv string         `yaml:"service_token_env"`
	HTTP            httpConfigFile `yaml:"http"`
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
	return LoadConfigFromPathWithValues(path, values)
}

func LoadConfigFromPathWithValues(path string, values sharedconfig.Values) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading projects config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing projects config %s: %w", path, err)
	}
	cfg, err := file.toConfig(values)
	if err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (f configFile) toConfig(values sharedconfig.Values) (*Config, error) {
	readHeaderTimeout, err := parseRequiredDuration("http.read_header_timeout", f.HTTP.ReadHeaderTimeout)
	if err != nil {
		return nil, err
	}
	return &Config{
		BindAddr:     f.BindAddr,
		DatabaseURL:  values.String(f.DatabaseURLEnv, ""),
		ServiceToken: values.String(f.ServiceTokenEnv, ""),
		HTTP:         HTTPConfig{ReadHeaderTimeout: readHeaderTimeout},
	}, nil
}

func (c *Config) validate() error {
	if c.BindAddr == "" {
		return errors.New("bind_addr is required")
	}
	if c.DatabaseURL == "" {
		return errors.New("database url is required")
	}
	if c.ServiceToken == "" {
		return errors.New("service token is required")
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		return errors.New("http.read_header_timeout must be positive")
	}
	return nil
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
	path := os.Getenv("PROJECTS_CONFIG_PATH")
	if path == "" {
		return "config/config.yaml"
	}
	return path
}
