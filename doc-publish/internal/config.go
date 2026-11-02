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
	BindAddr string
	Blog     BlogConfig
	Records  RecordsConfig
	Source   SourceConfig
	Wiki     WikiConfig
	Git      GitConfig
	HTTP     HTTPConfig
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
	// AssetDir holds the images of published wiki pages, one directory per
	// page. Required only when wiki publishing is enabled.
	AssetDir string
}

type RecordsConfig struct {
	Path string
}

type SourceConfig struct {
	DocumentsBaseURL string
	RequestTimeout   time.Duration
}

// WikiConfig enables publishing single den-wiki pages. An empty BaseURL
// disables it.
type WikiConfig struct {
	BaseURL        string
	RequestTimeout time.Duration
	MaxAssetBytes  int64
}

func (c WikiConfig) Enabled() bool {
	return c.BaseURL != ""
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
	Wiki     wikiConfigFile    `yaml:"wiki"`
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
	AssetDir          string `yaml:"asset_dir"`
}

type wikiConfigFile struct {
	BaseURL        string `yaml:"base_url"`
	RequestTimeout string `yaml:"request_timeout"`
	MaxAssetBytes  int64  `yaml:"max_asset_bytes"`
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
	var wikiTimeout time.Duration
	if f.Wiki.BaseURL != "" {
		wikiTimeout, err = parseRequiredDuration("wiki.request_timeout", f.Wiki.RequestTimeout)
		if err != nil {
			return nil, err
		}
	}
	return &Config{
		BindAddr: f.BindAddr,
		Blog: BlogConfig{
			RepoPath:          f.Blog.RepoPath,
			ExpectedRemoteURL: f.Blog.ExpectedRemoteURL,
			Branch:            f.Blog.Branch,
			PostDir:           f.Blog.PostDir,
			PublicBaseURL:     f.Blog.PublicBaseURL,
			AuthorName:        f.Blog.AuthorName,
			AuthorEmail:       f.Blog.AuthorEmail,
			Push:              f.Blog.Push,
			AssetDir:          f.Blog.AssetDir,
		},
		Records: RecordsConfig{Path: f.Records.Path},
		Source: SourceConfig{
			DocumentsBaseURL: f.Source.DocumentsBaseURL,
			RequestTimeout:   sourceTimeout,
		},
		Wiki: WikiConfig{
			BaseURL:        f.Wiki.BaseURL,
			RequestTimeout: wikiTimeout,
			MaxAssetBytes:  f.Wiki.MaxAssetBytes,
		},
		Git:  GitConfig{CommandTimeout: gitTimeout},
		HTTP: HTTPConfig{ReadHeaderTimeout: readHeaderTimeout},
	}, nil
}

func (c *Config) validate() error {
	if c.BindAddr == "" {
		return errors.New("bind_addr is required")
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
	if c.Wiki.Enabled() {
		if c.Blog.AssetDir == "" {
			return errors.New("blog.asset_dir is required when wiki.base_url is set")
		}
		if c.Wiki.RequestTimeout <= 0 || c.Wiki.MaxAssetBytes <= 0 {
			return errors.New("wiki.request_timeout and wiki.max_asset_bytes must be positive")
		}
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
