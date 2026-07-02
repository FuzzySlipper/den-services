package librarian

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	sharedconfig "den-services/shared/config"

	"gopkg.in/yaml.v3"
)

const (
	defaultTaskLimit      = 6
	defaultMessageLimit   = 8
	defaultDocumentLimit  = 8
	defaultKnowledgeLimit = 6
)

type Config struct {
	BindAddr         string
	ServiceToken     string
	ProjectsBaseURL  string
	ProjectsToken    string
	TasksBaseURL     string
	TasksToken       string
	MessagesBaseURL  string
	MessagesToken    string
	DocumentsBaseURL string
	DocumentsToken   string
	KnowledgeBaseURL string
	KnowledgeToken   string
	DefaultBudget    SourceLimits
	HTTP             HTTPConfig
	Upstreams        UpstreamsConfig
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
}

type UpstreamsConfig struct {
	RequestTimeout time.Duration
}

type configFile struct {
	BindAddr            string              `yaml:"bind_addr"`
	ServiceTokenEnv     string              `yaml:"service_token_env"`
	ProjectsBaseURLEnv  string              `yaml:"projects_base_url_env"`
	ProjectsTokenEnv    string              `yaml:"projects_token_env"`
	TasksBaseURLEnv     string              `yaml:"tasks_base_url_env"`
	TasksTokenEnv       string              `yaml:"tasks_token_env"`
	MessagesBaseURLEnv  string              `yaml:"messages_base_url_env"`
	MessagesTokenEnv    string              `yaml:"messages_token_env"`
	DocumentsBaseURLEnv string              `yaml:"documents_base_url_env"`
	DocumentsTokenEnv   string              `yaml:"documents_token_env"`
	KnowledgeBaseURLEnv string              `yaml:"knowledge_base_url_env"`
	KnowledgeTokenEnv   string              `yaml:"knowledge_token_env"`
	DefaultBudget       sourceLimitsFile    `yaml:"default_budget"`
	HTTP                httpConfigFile      `yaml:"http"`
	Upstreams           upstreamsConfigFile `yaml:"upstreams"`
}

type sourceLimitsFile struct {
	Tasks     int `yaml:"tasks"`
	Messages  int `yaml:"messages"`
	Documents int `yaml:"documents"`
	Knowledge int `yaml:"knowledge"`
}

type httpConfigFile struct {
	ReadHeaderTimeout string `yaml:"read_header_timeout"`
}

type upstreamsConfigFile struct {
	RequestTimeout string `yaml:"request_timeout"`
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
		return nil, fmt.Errorf("reading librarian config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing librarian config %s: %w", path, err)
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
	requestTimeout, err := parseRequiredDuration("upstreams.request_timeout", f.Upstreams.RequestTimeout)
	if err != nil {
		return nil, err
	}
	serviceToken := values.String(f.ServiceTokenEnv, "")
	return &Config{
		BindAddr:         f.BindAddr,
		ServiceToken:     serviceToken,
		ProjectsBaseURL:  values.String(f.ProjectsBaseURLEnv, ""),
		ProjectsToken:    values.String(f.ProjectsTokenEnv, serviceToken),
		TasksBaseURL:     values.String(f.TasksBaseURLEnv, ""),
		TasksToken:       values.String(f.TasksTokenEnv, serviceToken),
		MessagesBaseURL:  values.String(f.MessagesBaseURLEnv, ""),
		MessagesToken:    values.String(f.MessagesTokenEnv, serviceToken),
		DocumentsBaseURL: values.String(f.DocumentsBaseURLEnv, ""),
		DocumentsToken:   values.String(f.DocumentsTokenEnv, serviceToken),
		KnowledgeBaseURL: values.String(f.KnowledgeBaseURLEnv, ""),
		KnowledgeToken:   values.String(f.KnowledgeTokenEnv, serviceToken),
		DefaultBudget: SourceLimits{
			Tasks:     f.DefaultBudget.Tasks,
			Messages:  f.DefaultBudget.Messages,
			Documents: f.DefaultBudget.Documents,
			Knowledge: f.DefaultBudget.Knowledge,
		}.withDefaults(),
		HTTP:      HTTPConfig{ReadHeaderTimeout: readHeaderTimeout},
		Upstreams: UpstreamsConfig{RequestTimeout: requestTimeout},
	}, nil
}

func (c *Config) validate() error {
	required := map[string]string{
		"bind_addr": c.BindAddr, "service token": c.ServiceToken,
		"projects base url": c.ProjectsBaseURL, "projects token": c.ProjectsToken,
		"tasks base url": c.TasksBaseURL, "tasks token": c.TasksToken,
		"messages base url": c.MessagesBaseURL, "messages token": c.MessagesToken,
		"documents base url": c.DocumentsBaseURL, "documents token": c.DocumentsToken,
		"knowledge base url": c.KnowledgeBaseURL, "knowledge token": c.KnowledgeToken,
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		return errors.New("http.read_header_timeout must be positive")
	}
	if c.Upstreams.RequestTimeout <= 0 {
		return errors.New("upstreams.request_timeout must be positive")
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
	path := os.Getenv("LIBRARIAN_CONFIG_PATH")
	if path == "" {
		path = os.Getenv("DEN_LIBRARIAN_CONFIG_PATH")
	}
	if path == "" {
		return "config/config.yaml"
	}
	return path
}
