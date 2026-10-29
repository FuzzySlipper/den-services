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
		Backup       BackupConfig
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
		Backup       backupConfigFile   `yaml:"backup"`
	}
	backupConfigFile struct {
		Directory string `yaml:"directory"`
		At        string `yaml:"at"`
		Timezone  string `yaml:"timezone"`
		Keep      int    `yaml:"keep"`
		JobID     string `yaml:"job_id"`
		HostID    string `yaml:"host_id"`
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
	if cfg.Backup, err = file.Backup.parse(); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parse checks the backup section. Leaving out directory disables backups.
func (f backupConfigFile) parse() (BackupConfig, error) {
	if f.Directory == "" {
		return BackupConfig{}, nil
	}
	if _, err := time.Parse("15:04", f.At); err != nil {
		return BackupConfig{}, fmt.Errorf("backup.at must be HH:MM: %w", err)
	}
	location, err := time.LoadLocation(f.Timezone)
	if err != nil || f.Timezone == "" {
		return BackupConfig{}, fmt.Errorf("backup.timezone must be an IANA zone such as America/Los_Angeles: %q", f.Timezone)
	}
	if f.Keep < 1 {
		return BackupConfig{}, errors.New("backup.keep must be at least 1")
	}
	if err := validateID("backup.job_id", f.JobID); err != nil {
		return BackupConfig{}, err
	}
	if err := validateID("backup.host_id", f.HostID); err != nil {
		return BackupConfig{}, err
	}
	return BackupConfig{Directory: f.Directory, At: f.At, Location: location, Keep: f.Keep, JobID: f.JobID, HostID: f.HostID}, nil
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
