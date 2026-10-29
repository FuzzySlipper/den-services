package resources

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type (
	Config struct {
		BindAddr     string
		DatabasePath string
		HTTP         HTTPConfig
		StaleAfter   time.Duration
	}
	HTTPConfig struct{ ReadHeaderTimeout time.Duration }
	configFile struct {
		BindAddr     string         `yaml:"bind_addr"`
		DatabasePath string         `yaml:"database_path"`
		HTTP         httpConfigFile `yaml:"http"`
		StaleAfter   string         `yaml:"stale_after"`
	}
	httpConfigFile struct {
		ReadHeaderTimeout string `yaml:"read_header_timeout"`
	}
)

func LoadConfig() (*Config, error) { return LoadConfigFromPath(configPath()) }

func LoadConfigFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading resources config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing resources config %s: %w", path, err)
	}
	readHeaderTimeout, err := time.ParseDuration(file.HTTP.ReadHeaderTimeout)
	if err != nil {
		return nil, fmt.Errorf("parsing http.read_header_timeout: %w", err)
	}
	staleAfter, err := time.ParseDuration(file.StaleAfter)
	if err != nil {
		return nil, fmt.Errorf("parsing stale_after: %w", err)
	}
	cfg := &Config{
		BindAddr: file.BindAddr, DatabasePath: file.DatabasePath,
		HTTP: HTTPConfig{ReadHeaderTimeout: readHeaderTimeout}, StaleAfter: staleAfter,
	}
	switch {
	case cfg.BindAddr == "":
		return nil, errors.New("bind_addr is required")
	case cfg.DatabasePath == "":
		return nil, errors.New("database_path is required")
	case cfg.HTTP.ReadHeaderTimeout <= 0:
		return nil, errors.New("http.read_header_timeout must be positive")
	case cfg.StaleAfter <= 0:
		return nil, errors.New("stale_after must be positive")
	}
	return cfg, nil
}

func configPath() string {
	if path := os.Getenv("RESOURCES_CONFIG_PATH"); path != "" {
		return path
	}
	return "config/config.yaml"
}
