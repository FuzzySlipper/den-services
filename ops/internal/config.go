package ops

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
		Defaults     DefaultsConfig
	}
	HTTPConfig struct{ ReadHeaderTimeout time.Duration }
	// DefaultsConfig fills job fields a registration leaves out.
	DefaultsConfig struct {
		HeartbeatStaleAfter time.Duration
		RunListLimit        int
	}
	configFile struct {
		BindAddr     string             `yaml:"bind_addr"`
		DatabasePath string             `yaml:"database_path"`
		HTTP         httpConfigFile     `yaml:"http"`
		Defaults     defaultsConfigFile `yaml:"defaults"`
	}
	httpConfigFile struct {
		ReadHeaderTimeout string `yaml:"read_header_timeout"`
	}
	defaultsConfigFile struct {
		HeartbeatStaleAfter string `yaml:"heartbeat_stale_after"`
		RunListLimit        int    `yaml:"run_list_limit"`
	}
)

func LoadConfig() (*Config, error) { return LoadConfigFromPath(configPath()) }

func LoadConfigFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading ops config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing ops config %s: %w", path, err)
	}
	readHeaderTimeout, err := time.ParseDuration(file.HTTP.ReadHeaderTimeout)
	if err != nil {
		return nil, fmt.Errorf("parsing http.read_header_timeout: %w", err)
	}
	staleAfter, err := time.ParseDuration(file.Defaults.HeartbeatStaleAfter)
	if err != nil {
		return nil, fmt.Errorf("parsing defaults.heartbeat_stale_after: %w", err)
	}
	cfg := &Config{
		BindAddr:     file.BindAddr,
		DatabasePath: file.DatabasePath,
		HTTP:         HTTPConfig{ReadHeaderTimeout: readHeaderTimeout},
		Defaults:     DefaultsConfig{HeartbeatStaleAfter: staleAfter, RunListLimit: file.Defaults.RunListLimit},
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	switch {
	case c.BindAddr == "":
		return errors.New("bind_addr is required")
	case c.DatabasePath == "":
		return errors.New("database_path is required")
	case c.HTTP.ReadHeaderTimeout <= 0:
		return errors.New("http.read_header_timeout must be positive")
	case c.Defaults.HeartbeatStaleAfter <= 0:
		return errors.New("defaults.heartbeat_stale_after must be positive")
	case c.Defaults.RunListLimit <= 0 || c.Defaults.RunListLimit > MaxListLimit:
		return fmt.Errorf("defaults.run_list_limit must be between 1 and %d", MaxListLimit)
	}
	return nil
}

func configPath() string {
	if path := os.Getenv("OPS_CONFIG_PATH"); path != "" {
		return path
	}
	return "config/config.yaml"
}
