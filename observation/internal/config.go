package observation

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
	DefaultLimit int
	MaxLimit     int
	ChatSource   ChatSourceConfig
	Retention    RetentionConfig
	HTTP         HTTPConfig
}

type ChatSourceMode string

const (
	ChatSourceModePostgresView ChatSourceMode = "postgres_view"
	ChatSourceModeLegacyHTTP   ChatSourceMode = "legacy_http"
)

type ChatSourceConfig struct {
	Mode                  ChatSourceMode
	LegacyChannelsBaseURL string
	LegacyHTTPTimeout     time.Duration
}

type RetentionConfig struct {
	ActivityTTL time.Duration
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
}

type configFile struct {
	BindAddr    string               `yaml:"bind_addr"`
	DatabaseURL string               `yaml:"database_url"`
	Query       queryConfigFile      `yaml:"query"`
	ChatSource  chatSourceConfigFile `yaml:"chat_source"`
	Retention   retentionConfigFile  `yaml:"retention"`
	HTTP        httpConfigFile       `yaml:"http"`
}

type queryConfigFile struct {
	DefaultLimit int `yaml:"default_limit"`
	MaxLimit     int `yaml:"max_limit"`
}

type chatSourceConfigFile struct {
	Mode                  string `yaml:"mode"`
	LegacyChannelsBaseURL string `yaml:"legacy_channels_base_url"`
	LegacyHTTPTimeout     string `yaml:"legacy_http_timeout"`
}

type retentionConfigFile struct {
	ActivityTTL string `yaml:"activity_ttl"`
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
		return nil, fmt.Errorf("reading observation config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing observation config %s: %w", path, err)
	}
	databaseURL, err := values.Expand(file.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("expanding database_url: %w", err)
	}
	retention, err := file.Retention.toConfig()
	if err != nil {
		return nil, err
	}
	httpConfig, err := file.HTTP.toConfig()
	if err != nil {
		return nil, err
	}
	chatSource, err := file.ChatSource.toConfig()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		BindAddr:     file.BindAddr,
		DatabaseURL:  databaseURL,
		ServiceToken: os.Getenv("DEN_OBSERVATION_SERVICE_TOKEN"),
		DefaultLimit: file.Query.DefaultLimit,
		MaxLimit:     file.Query.MaxLimit,
		ChatSource:   chatSource,
		Retention:    retention,
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
	if c.DefaultLimit <= 0 || c.MaxLimit <= 0 || c.DefaultLimit > c.MaxLimit {
		return errors.New("query limits must be positive and default_limit must not exceed max_limit")
	}
	if c.Retention.ActivityTTL <= 0 {
		return errors.New("retention.activity_ttl must be positive")
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		return errors.New("http read_header_timeout must be positive")
	}
	if c.ChatSource.Mode != ChatSourceModePostgresView && c.ChatSource.Mode != ChatSourceModeLegacyHTTP {
		return errors.New("chat_source.mode must be postgres_view or legacy_http")
	}
	if c.ChatSource.Mode == ChatSourceModeLegacyHTTP && c.ChatSource.LegacyChannelsBaseURL == "" {
		return errors.New("chat_source.legacy_channels_base_url is required when mode is legacy_http")
	}
	if c.ChatSource.Mode == ChatSourceModeLegacyHTTP && c.ChatSource.LegacyHTTPTimeout <= 0 {
		return errors.New("chat_source.legacy_http_timeout must be positive when mode is legacy_http")
	}
	return nil
}

func (c chatSourceConfigFile) toConfig() (ChatSourceConfig, error) {
	mode := ChatSourceMode(c.Mode)
	if mode == "" {
		mode = ChatSourceModePostgresView
	}
	legacyTimeout := time.Duration(0)
	if c.LegacyHTTPTimeout != "" {
		parsed, err := time.ParseDuration(c.LegacyHTTPTimeout)
		if err != nil {
			return ChatSourceConfig{}, fmt.Errorf("parsing chat_source.legacy_http_timeout: %w", err)
		}
		legacyTimeout = parsed
	}
	if mode == ChatSourceModeLegacyHTTP && legacyTimeout == 0 {
		legacyTimeout = 5 * time.Second
	}
	return ChatSourceConfig{
		Mode:                  mode,
		LegacyChannelsBaseURL: c.LegacyChannelsBaseURL,
		LegacyHTTPTimeout:     legacyTimeout,
	}, nil
}

func (c retentionConfigFile) toConfig() (RetentionConfig, error) {
	activityTTL, err := parseRequiredDuration("retention.activity_ttl", c.ActivityTTL)
	if err != nil {
		return RetentionConfig{}, err
	}
	return RetentionConfig{ActivityTTL: activityTTL}, nil
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
	path := os.Getenv("OBSERVATION_CONFIG_PATH")
	if path == "" {
		return "config/config.yaml"
	}
	return path
}
