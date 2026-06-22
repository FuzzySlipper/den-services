package docpublish

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
	ServiceToken string
	SourceToken  string
	Blog         BlogConfig
	Records      RecordsConfig
	Source       SourceConfig
	Git          GitConfig
	HTTP         HTTPConfig
}

type BlogConfig struct {
	RepoPath          string
	ExpectedRemoteURL string
	Branch            string
	PostDir           string
	PublicBaseURL     string
	AuthorName        string
	AuthorEmail       string
	Push              bool
}

type RecordsConfig struct {
	Path string
}

type SourceConfig struct {
	DocumentsBaseURL string
	RequestTimeout   time.Duration
}

type GitConfig struct {
	CommandTimeout time.Duration
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
}

type configFile struct {
	BindAddr string            `yaml:"bind_addr"`
	Blog     blogConfigFile    `yaml:"blog"`
	Records  recordsConfigFile `yaml:"records"`
	Source   sourceConfigFile  `yaml:"source"`
	Git      gitConfigFile     `yaml:"git"`
	HTTP     httpConfigFile    `yaml:"http"`
}

type blogConfigFile struct {
	RepoPath          string `yaml:"repo_path"`
	ExpectedRemoteURL string `yaml:"expected_remote_url"`
	Branch            string `yaml:"branch"`
	PostDir           string `yaml:"post_dir"`
	PublicBaseURL     string `yaml:"public_base_url"`
	AuthorName        string `yaml:"author_name"`
	AuthorEmail       string `yaml:"author_email"`
	Push              bool   `yaml:"push"`
}

type recordsConfigFile struct {
	Path string `yaml:"path"`
}

type sourceConfigFile struct {
	DocumentsBaseURL string `yaml:"documents_base_url"`
	RequestTimeout   string `yaml:"request_timeout"`
}

type gitConfigFile struct {
	CommandTimeout string `yaml:"command_timeout"`
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
		return nil, fmt.Errorf("reading doc-publish config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing doc-publish config %s: %w", path, err)
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
	sourceTimeout, err := parseRequiredDuration("source.request_timeout", f.Source.RequestTimeout)
	if err != nil {
		return nil, err
	}
	gitTimeout, err := parseRequiredDuration("git.command_timeout", f.Git.CommandTimeout)
	if err != nil {
		return nil, err
	}
	readHeaderTimeout, err := parseRequiredDuration("http.read_header_timeout", f.HTTP.ReadHeaderTimeout)
	if err != nil {
		return nil, err
	}
	return &Config{
		BindAddr:     f.BindAddr,
		ServiceToken: values.String("DEN_DOC_PUBLISH_SERVICE_TOKEN", ""),
		SourceToken:  values.String("DEN_DOC_PUBLISH_SOURCE_TOKEN", ""),
		Blog: BlogConfig{
			RepoPath:          f.Blog.RepoPath,
			ExpectedRemoteURL: f.Blog.ExpectedRemoteURL,
			Branch:            f.Blog.Branch,
			PostDir:           f.Blog.PostDir,
			PublicBaseURL:     f.Blog.PublicBaseURL,
			AuthorName:        f.Blog.AuthorName,
			AuthorEmail:       f.Blog.AuthorEmail,
			Push:              f.Blog.Push,
		},
		Records: RecordsConfig{Path: f.Records.Path},
		Source: SourceConfig{
			DocumentsBaseURL: f.Source.DocumentsBaseURL,
			RequestTimeout:   sourceTimeout,
		},
		Git:  GitConfig{CommandTimeout: gitTimeout},
		HTTP: HTTPConfig{ReadHeaderTimeout: readHeaderTimeout},
	}, nil
}

func (c *Config) validate() error {
	if c.BindAddr == "" {
		return errors.New("bind_addr is required")
	}
	if c.ServiceToken == "" {
		return errors.New("service token is required")
	}
	if c.Blog.RepoPath == "" || c.Blog.ExpectedRemoteURL == "" || c.Blog.Branch == "" || c.Blog.PostDir == "" || c.Blog.PublicBaseURL == "" {
		return errors.New("blog repo_path, expected_remote_url, branch, post_dir, and public_base_url are required")
	}
	if c.Blog.AuthorName == "" || c.Blog.AuthorEmail == "" {
		return errors.New("blog author_name and author_email are required")
	}
	if c.Records.Path == "" {
		return errors.New("records.path is required")
	}
	if c.Source.DocumentsBaseURL == "" {
		return errors.New("source.documents_base_url is required")
	}
	if c.Source.RequestTimeout <= 0 || c.Git.CommandTimeout <= 0 || c.HTTP.ReadHeaderTimeout <= 0 {
		return errors.New("timeouts must be positive")
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
	path := os.Getenv("DOC_PUBLISH_CONFIG_PATH")
	if path == "" {
		return "config/config.yaml"
	}
	return path
}
