package conversation

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
	DefaultLimit int
	MaxLimit     int
	HTTP         HTTPConfig
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
}

type configFile struct {
	BindAddr    string          `yaml:"bind_addr"`
	DatabaseURL string          `yaml:"database_url"`
	Query       queryConfigFile `yaml:"query"`
	HTTP        httpConfigFile  `yaml:"http"`
}

type queryConfigFile struct {
	DefaultLimit int `yaml:"default_limit"`
	MaxLimit     int `yaml:"max_limit"`
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
		return nil, fmt.Errorf("reading conversation config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing conversation config %s: %w", path, err)
	}
	databaseURL, err := values.Expand(file.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("expanding database_url: %w", err)
	}
	httpConfig, err := file.HTTP.toConfig()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		BindAddr:     file.BindAddr,
		DatabaseURL:  databaseURL,
		ServiceToken: values.String("DEN_CONVERSATION_SERVICE_TOKEN", ""),
		DefaultLimit: file.Query.DefaultLimit,
		MaxLimit:     file.Query.MaxLimit,
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
	if c.ServiceToken == "" {
		return ErrMissingServiceToken
	}
	if c.DefaultLimit <= 0 || c.MaxLimit <= 0 || c.DefaultLimit > c.MaxLimit {
		return errors.New("query limits must be positive and default_limit must not exceed max_limit")
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		return errors.New("http read_header_timeout must be positive")
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
	path := os.Getenv("CONVERSATION_CONFIG_PATH")
	if path == "" {
		return "config/config.yaml"
	}
	return path
}
