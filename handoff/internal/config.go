package handoff

import (
	"errors"
	"fmt"
	"os"
	"time"

	sharedconfig "den-services/shared/config"
	"gopkg.in/yaml.v3"
)

type (
	Config struct {
		BindAddr, DatabaseURL, ServiceToken string
		HTTP                                HTTPConfig
	}
	HTTPConfig struct{ ReadHeaderTimeout time.Duration }
	configFile struct {
		BindAddr        string         `yaml:"bind_addr"`
		DatabaseURLEnv  string         `yaml:"database_url_env"`
		ServiceTokenEnv string         `yaml:"service_token_env"`
		HTTP            httpConfigFile `yaml:"http"`
	}
	httpConfigFile struct {
		ReadHeaderTimeout string `yaml:"read_header_timeout"`
	}
)

func LoadConfig() (*Config, error) { return LoadConfigFromPath(configPath()) }
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
		return nil, fmt.Errorf("reading handoff config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing handoff config %s: %w", path, err)
	}
	duration, err := time.ParseDuration(file.HTTP.ReadHeaderTimeout)
	if err != nil {
		return nil, fmt.Errorf("parsing http.read_header_timeout: %w", err)
	}
	cfg := &Config{BindAddr: file.BindAddr, DatabaseURL: values.String(file.DatabaseURLEnv, ""), ServiceToken: values.String(file.ServiceTokenEnv, ""), HTTP: HTTPConfig{ReadHeaderTimeout: duration}}
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
func configPath() string {
	if path := os.Getenv("HANDOFF_CONFIG_PATH"); path != "" {
		return path
	}
	return "config/config.yaml"
}
