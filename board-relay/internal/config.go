package boardrelay

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	sharedconfig "den-services/shared/config"

	"gopkg.in/yaml.v3"
)

type Config struct {
	BindAddr     string
	DatabaseURL  string
	ServiceToken string
	BoardBaseURL string
	BoardToken   string
	GitHub       GitHubConfig
	HTTP         HTTPConfig
}

type GitHubConfig struct {
	APIBaseURL     string
	Repository     string
	Token          string
	RequestTimeout time.Duration
}

type HTTPConfig struct{ ReadHeaderTimeout time.Duration }

type configFile struct {
	BindAddr        string         `yaml:"bind_addr"`
	DatabaseURLEnv  string         `yaml:"database_url_env"`
	ServiceTokenEnv string         `yaml:"service_token_env"`
	BoardBaseURL    string         `yaml:"board_base_url"`
	BoardTokenEnv   string         `yaml:"board_token_env"`
	GitHub          githubFile     `yaml:"github"`
	HTTP            httpConfigFile `yaml:"http"`
}

type githubFile struct {
	APIBaseURL     string `yaml:"api_base_url"`
	Repository     string `yaml:"repository"`
	TokenEnv       string `yaml:"token_env"`
	RequestTimeout string `yaml:"request_timeout"`
}

type httpConfigFile struct {
	ReadHeaderTimeout string `yaml:"read_header_timeout"`
}

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
		return nil, fmt.Errorf("reading board relay config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing board relay config %s: %w", path, err)
	}
	githubTimeout, err := time.ParseDuration(file.GitHub.RequestTimeout)
	if err != nil {
		return nil, fmt.Errorf("parsing github.request_timeout: %w", err)
	}
	readHeaderTimeout, err := time.ParseDuration(file.HTTP.ReadHeaderTimeout)
	if err != nil {
		return nil, fmt.Errorf("parsing http.read_header_timeout: %w", err)
	}
	cfg := &Config{
		BindAddr: strings.TrimSpace(file.BindAddr), DatabaseURL: values.String(file.DatabaseURLEnv, ""), ServiceToken: values.String(file.ServiceTokenEnv, ""),
		BoardBaseURL: strings.TrimRight(strings.TrimSpace(file.BoardBaseURL), "/"), BoardToken: values.String(file.BoardTokenEnv, ""),
		GitHub: GitHubConfig{APIBaseURL: strings.TrimRight(strings.TrimSpace(file.GitHub.APIBaseURL), "/"), Repository: strings.TrimSpace(file.GitHub.Repository), Token: values.String(file.GitHub.TokenEnv, ""), RequestTimeout: githubTimeout},
		HTTP:   HTTPConfig{ReadHeaderTimeout: readHeaderTimeout},
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if c == nil || c.BindAddr == "" || c.DatabaseURL == "" || c.ServiceToken == "" || c.BoardBaseURL == "" || c.BoardToken == "" || c.GitHub.APIBaseURL == "" || c.GitHub.Token == "" {
		return errors.New("board relay required configuration is missing")
	}
	if _, err := normalizeRepository(c.GitHub.Repository); err != nil {
		return err
	}
	if c.GitHub.RequestTimeout <= 0 || c.HTTP.ReadHeaderTimeout <= 0 {
		return errors.New("board relay timeouts must be positive")
	}
	return nil
}

func configPath() string {
	if path := os.Getenv("BOARD_RELAY_CONFIG_PATH"); path != "" {
		return path
	}
	return "config/config.yaml"
}
