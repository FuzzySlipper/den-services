package artifacts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	sharedconfig "den-services/shared/config"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BindAddr     string
	DatabaseURL  string
	ServiceToken string
	Storage      StorageConfig
	Limits       LimitConfig
	Retention    RetentionConfig
	HTTP         HTTPConfig
}

type StorageConfig struct {
	Backend   string
	RootPath  string
	KeyPrefix string
}

type LimitConfig struct {
	MaxBytesPerArtifact int64
	MaxPixelsPerImage   int64
}

type RetentionConfig struct {
	DefaultTTL   time.Duration
	TemporaryTTL time.Duration
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
}

type configFile struct {
	BindAddr        string              `yaml:"bind_addr"`
	DatabaseURLEnv  string              `yaml:"database_url_env"`
	ServiceTokenEnv string              `yaml:"service_token_env"`
	Storage         storageConfigFile   `yaml:"storage"`
	Limits          limitConfigFile     `yaml:"limits"`
	Retention       retentionConfigFile `yaml:"retention"`
	HTTP            httpConfigFile      `yaml:"http"`
}

type storageConfigFile struct {
	Backend   string `yaml:"backend"`
	RootPath  string `yaml:"root_path"`
	KeyPrefix string `yaml:"key_prefix"`
}

type limitConfigFile struct {
	MaxBytesPerArtifact int64 `yaml:"max_bytes_per_artifact"`
	MaxPixelsPerImage   int64 `yaml:"max_pixels_per_image"`
}

type retentionConfigFile struct {
	DefaultTTL   string `yaml:"default_ttl"`
	TemporaryTTL string `yaml:"temporary_ttl"`
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
		return nil, fmt.Errorf("reading artifacts config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing artifacts config %s: %w", path, err)
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
	defaultTTL, err := parseRequiredDuration("retention.default_ttl", f.Retention.DefaultTTL)
	if err != nil {
		return nil, err
	}
	temporaryTTL, err := parseRequiredDuration("retention.temporary_ttl", f.Retention.TemporaryTTL)
	if err != nil {
		return nil, err
	}
	readHeaderTimeout, err := parseRequiredDuration("http.read_header_timeout", f.HTTP.ReadHeaderTimeout)
	if err != nil {
		return nil, err
	}
	return &Config{
		BindAddr:     f.BindAddr,
		DatabaseURL:  values.String(f.DatabaseURLEnv, ""),
		ServiceToken: values.String(f.ServiceTokenEnv, ""),
		Storage: StorageConfig{
			Backend:   f.Storage.Backend,
			RootPath:  filepath.Clean(f.Storage.RootPath),
			KeyPrefix: f.Storage.KeyPrefix,
		},
		Limits: LimitConfig{
			MaxBytesPerArtifact: f.Limits.MaxBytesPerArtifact,
			MaxPixelsPerImage:   f.Limits.MaxPixelsPerImage,
		},
		Retention: RetentionConfig{
			DefaultTTL:   defaultTTL,
			TemporaryTTL: temporaryTTL,
		},
		HTTP: HTTPConfig{ReadHeaderTimeout: readHeaderTimeout},
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
	if c.Storage.Backend != "filesystem" {
		return errors.New("storage.backend must be filesystem")
	}
	if !filepath.IsAbs(c.Storage.RootPath) {
		return errors.New("storage.root_path must be absolute")
	}
	if c.Storage.KeyPrefix == "" {
		return errors.New("storage.key_prefix is required")
	}
	if c.Limits.MaxBytesPerArtifact <= 0 || c.Limits.MaxPixelsPerImage <= 0 {
		return errors.New("limits must be positive")
	}
	if c.Retention.DefaultTTL < 0 || c.Retention.TemporaryTTL <= 0 {
		return errors.New("retention durations are invalid")
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
	path := os.Getenv("ARTIFACTS_CONFIG_PATH")
	if path == "" {
		return "config/config.yaml"
	}
	return path
}
