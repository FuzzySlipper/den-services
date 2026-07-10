package review

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
	BindAddr                     string
	DatabaseURL                  string
	ServiceToken                 string
	AllowUnauthenticatedLocalDev bool
	ProjectsBaseURL              string
	ProjectsToken                string
	TasksBaseURL                 string
	TasksToken                   string
	MessagesBaseURL              string
	MessagesToken                string
	HTTP                         HTTPConfig
	GitHub                       GitHubConfig
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
}

type GitHubConfig struct {
	Enabled           bool
	APIBaseURL        string
	Token             string
	PollInterval      time.Duration
	MissingCheckGrace time.Duration
	RequestTimeout    time.Duration
	DefaultTimeout    time.Duration
	MaxTimeout        time.Duration
	BatchSize         int
	StatusURLBase     string
}

type configFile struct {
	BindAddr                     string           `yaml:"bind_addr"`
	DatabaseURLEnv               string           `yaml:"database_url_env"`
	ServiceTokenEnv              string           `yaml:"service_token_env"`
	AllowUnauthenticatedLocalDev bool             `yaml:"allow_unauthenticated_local_dev"`
	ProjectsBaseURLEnv           string           `yaml:"projects_base_url_env"`
	ProjectsTokenEnv             string           `yaml:"projects_token_env"`
	TasksBaseURLEnv              string           `yaml:"tasks_base_url_env"`
	TasksTokenEnv                string           `yaml:"tasks_token_env"`
	MessagesBaseURLEnv           string           `yaml:"messages_base_url_env"`
	MessagesTokenEnv             string           `yaml:"messages_token_env"`
	HTTP                         httpConfigFile   `yaml:"http"`
	GitHub                       githubConfigFile `yaml:"github"`
}

type httpConfigFile struct {
	ReadHeaderTimeout string `yaml:"read_header_timeout"`
}

type githubConfigFile struct {
	Enabled           bool   `yaml:"enabled"`
	APIBaseURL        string `yaml:"api_base_url"`
	TokenEnv          string `yaml:"token_env"`
	PollInterval      string `yaml:"poll_interval"`
	MissingCheckGrace string `yaml:"missing_check_grace"`
	RequestTimeout    string `yaml:"request_timeout"`
	DefaultTimeout    string `yaml:"default_timeout"`
	MaxTimeout        string `yaml:"max_timeout"`
	BatchSize         int    `yaml:"batch_size"`
	StatusURLBase     string `yaml:"status_url_base"`
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
		return nil, fmt.Errorf("reading review config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing review config %s: %w", path, err)
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
	github, err := f.GitHub.toConfig(values)
	if err != nil {
		return nil, err
	}
	serviceToken := values.String(f.ServiceTokenEnv, "")
	return &Config{
		BindAddr:                     f.BindAddr,
		DatabaseURL:                  values.String(f.DatabaseURLEnv, ""),
		ServiceToken:                 serviceToken,
		AllowUnauthenticatedLocalDev: f.AllowUnauthenticatedLocalDev,
		ProjectsBaseURL:              values.String(f.ProjectsBaseURLEnv, ""),
		ProjectsToken:                values.String(f.ProjectsTokenEnv, serviceToken),
		TasksBaseURL:                 values.String(f.TasksBaseURLEnv, ""),
		TasksToken:                   values.String(f.TasksTokenEnv, serviceToken),
		MessagesBaseURL:              values.String(f.MessagesBaseURLEnv, ""),
		MessagesToken:                values.String(f.MessagesTokenEnv, serviceToken),
		HTTP:                         HTTPConfig{ReadHeaderTimeout: readHeaderTimeout},
		GitHub:                       github,
	}, nil
}

func (c *Config) validate() error {
	required := map[string]string{
		"bind_addr": c.BindAddr, "database url": c.DatabaseURL, "service token": c.ServiceToken,
		"projects base url": c.ProjectsBaseURL, "projects token": c.ProjectsToken,
		"tasks base url": c.TasksBaseURL, "tasks token": c.TasksToken,
		"messages base url": c.MessagesBaseURL, "messages token": c.MessagesToken,
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		return errors.New("http.read_header_timeout must be positive")
	}
	if err := c.GitHub.validate(); err != nil {
		return err
	}
	return nil
}

func (f githubConfigFile) toConfig(values sharedconfig.Values) (GitHubConfig, error) {
	pollInterval, err := parseOptionalDuration("github.poll_interval", f.PollInterval, defaultGitHubCheckPollInterval)
	if err != nil {
		return GitHubConfig{}, err
	}
	missingCheckGrace, err := parseOptionalDuration("github.missing_check_grace", f.MissingCheckGrace, defaultGitHubMissingCheckGrace)
	if err != nil {
		return GitHubConfig{}, err
	}
	requestTimeout, err := parseOptionalDuration("github.request_timeout", f.RequestTimeout, 10*time.Second)
	if err != nil {
		return GitHubConfig{}, err
	}
	defaultTimeout, err := parseOptionalDuration("github.default_timeout", f.DefaultTimeout, DefaultGitHubCheckOptions().DefaultTimeout)
	if err != nil {
		return GitHubConfig{}, err
	}
	maxTimeout, err := parseOptionalDuration("github.max_timeout", f.MaxTimeout, DefaultGitHubCheckOptions().MaxTimeout)
	if err != nil {
		return GitHubConfig{}, err
	}
	tokenEnv := strings.TrimSpace(f.TokenEnv)
	apiBaseURL := strings.TrimRight(strings.TrimSpace(f.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = "https://api.github.com"
	}
	batchSize := f.BatchSize
	if batchSize == 0 {
		batchSize = 10
	}
	return GitHubConfig{
		Enabled: f.Enabled, APIBaseURL: apiBaseURL,
		Token: values.String(tokenEnv, ""), PollInterval: pollInterval, MissingCheckGrace: missingCheckGrace, RequestTimeout: requestTimeout,
		DefaultTimeout: defaultTimeout, MaxTimeout: maxTimeout, BatchSize: batchSize,
		StatusURLBase: strings.TrimRight(strings.TrimSpace(f.StatusURLBase), "/"),
	}, nil
}

func (c GitHubConfig) validate() error {
	if !c.Enabled {
		return nil
	}
	if c.APIBaseURL == "" {
		return errors.New("github.api_base_url is required when github.enabled is true")
	}
	if c.PollInterval <= 0 {
		return errors.New("github.poll_interval must be positive")
	}
	if c.MissingCheckGrace <= 0 {
		return errors.New("github.missing_check_grace must be positive")
	}
	if c.RequestTimeout <= 0 {
		return errors.New("github.request_timeout must be positive")
	}
	if c.DefaultTimeout <= 0 {
		return errors.New("github.default_timeout must be positive")
	}
	if c.MaxTimeout <= 0 || c.MaxTimeout < c.DefaultTimeout {
		return errors.New("github.max_timeout must be at least github.default_timeout")
	}
	if c.BatchSize <= 0 {
		return errors.New("github.batch_size must be positive")
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

func parseOptionalDuration(name string, raw string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", name, err)
	}
	return parsed, nil
}

func configPath() string {
	path := os.Getenv("REVIEW_CONFIG_PATH")
	if path == "" {
		return "config/config.yaml"
	}
	return path
}
