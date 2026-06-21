package visualcontract

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
	ServiceToken string
	Artifacts    ArtifactConfig
	HTTP         HTTPConfig
}

type ArtifactConfig struct {
	BaseURL string
	Path    string
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
}

type configFile struct {
	BindAddr  string             `yaml:"bind_addr"`
	Artifacts artifactConfigFile `yaml:"artifacts"`
	HTTP      httpConfigFile     `yaml:"http"`
}

type artifactConfigFile struct {
	BaseURL string `yaml:"base_url"`
	Path    string `yaml:"path"`
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
		return nil, fmt.Errorf("reading visual-contract config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing visual-contract config %s: %w", path, err)
	}
	httpConfig, err := file.HTTP.toConfig()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		BindAddr:     file.BindAddr,
		ServiceToken: values.String("DEN_VISUAL_CONTRACT_SERVICE_TOKEN", ""),
		Artifacts: ArtifactConfig{
			BaseURL: file.Artifacts.BaseURL,
			Path:    file.Artifacts.Path,
		},
		HTTP: httpConfig,
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
	if c.ServiceToken == "" {
		return errors.New("service token is required")
	}
	if c.Artifacts.BaseURL == "" {
		return errors.New("artifacts.base_url is required")
	}
	if c.Artifacts.Path == "" {
		return errors.New("artifacts.path is required")
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
	path := os.Getenv("VISUAL_CONTRACT_CONFIG_PATH")
	if path == "" {
		return "config/config.yaml"
	}
	return path
}
