package webedge

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	sharedconfig "den-services/shared/config"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig
	Static  StaticConfig
	Gateway GatewayConfig
}

type ServerConfig struct {
	ListenAddr        string
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
}

type StaticConfig struct {
	Root                 string
	RuntimeConfigPath    string
	BuildSentinelPath    string
	ImmutableAssetMaxAge time.Duration
}

type GatewayConfig struct {
	BaseURL               *url.URL
	BearerTokenEnv        string
	BearerToken           string
	ResponseHeaderTimeout time.Duration
}

type configFile struct {
	Server  serverConfigFile  `yaml:"server"`
	Static  staticConfigFile  `yaml:"static"`
	Gateway gatewayConfigFile `yaml:"gateway"`
}

type serverConfigFile struct {
	ListenAddr        string `yaml:"listen_addr"`
	ReadHeaderTimeout string `yaml:"read_header_timeout"`
	IdleTimeout       string `yaml:"idle_timeout"`
}

type staticConfigFile struct {
	Root                 string `yaml:"root"`
	RuntimeConfigFile    string `yaml:"runtime_config_file"`
	BuildSentinelFile    string `yaml:"build_sentinel_file"`
	ImmutableAssetMaxAge string `yaml:"immutable_asset_max_age"`
}

type gatewayConfigFile struct {
	BaseURL               string `yaml:"base_url"`
	BearerTokenEnv        string `yaml:"bearer_token_env"`
	ResponseHeaderTimeout string `yaml:"response_header_timeout"`
}

func Load() (*Config, error) {
	path := strings.TrimSpace(os.Getenv("WEB_EDGE_CONFIG_PATH"))
	if path == "" {
		path = "/etc/den-services/web-edge.yaml"
	}
	values, err := sharedconfig.Load()
	if err != nil {
		return nil, err
	}
	return LoadFromPathWithValues(path, values)
}

func LoadFromPathWithValues(path string, values sharedconfig.Values) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading web-edge config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing web-edge config %s: %w", path, err)
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
	readHeaderTimeout, err := parseDuration("server.read_header_timeout", f.Server.ReadHeaderTimeout)
	if err != nil {
		return nil, err
	}
	idleTimeout, err := parseDuration("server.idle_timeout", f.Server.IdleTimeout)
	if err != nil {
		return nil, err
	}
	immutableAssetMaxAge, err := parseDuration("static.immutable_asset_max_age", f.Static.ImmutableAssetMaxAge)
	if err != nil {
		return nil, err
	}
	responseHeaderTimeout, err := parseDuration("gateway.response_header_timeout", f.Gateway.ResponseHeaderTimeout)
	if err != nil {
		return nil, err
	}
	baseURL, err := url.Parse(strings.TrimSpace(f.Gateway.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("parsing gateway.base_url: %w", err)
	}
	tokenEnv := strings.TrimSpace(f.Gateway.BearerTokenEnv)
	token, err := values.RequiredString(tokenEnv)
	if err != nil {
		return nil, fmt.Errorf("loading gateway bearer token from %s: %w", tokenEnv, err)
	}
	root := filepath.Clean(strings.TrimSpace(f.Static.Root))
	return &Config{
		Server: ServerConfig{
			ListenAddr:        strings.TrimSpace(f.Server.ListenAddr),
			ReadHeaderTimeout: readHeaderTimeout,
			IdleTimeout:       idleTimeout,
		},
		Static: StaticConfig{
			Root:                 root,
			RuntimeConfigPath:    filepath.Join(root, strings.TrimSpace(f.Static.RuntimeConfigFile)),
			BuildSentinelPath:    filepath.Join(root, strings.TrimSpace(f.Static.BuildSentinelFile)),
			ImmutableAssetMaxAge: immutableAssetMaxAge,
		},
		Gateway: GatewayConfig{
			BaseURL:               baseURL,
			BearerTokenEnv:        tokenEnv,
			BearerToken:           token,
			ResponseHeaderTimeout: responseHeaderTimeout,
		},
	}, nil
}

func (c *Config) validate() error {
	if c.Server.ListenAddr == "" {
		return errors.New("server.listen_addr is required")
	}
	if c.Server.ReadHeaderTimeout <= 0 || c.Server.IdleTimeout <= 0 {
		return errors.New("server timeouts must be positive")
	}
	if !filepath.IsAbs(c.Static.Root) {
		return errors.New("static.root must be an absolute path")
	}
	if c.Static.ImmutableAssetMaxAge <= 0 {
		return errors.New("static.immutable_asset_max_age must be positive")
	}
	if c.Gateway.BaseURL.Scheme != "http" && c.Gateway.BaseURL.Scheme != "https" {
		return errors.New("gateway.base_url must use http or https")
	}
	if c.Gateway.BaseURL.Host == "" || c.Gateway.BaseURL.Path != "" ||
		c.Gateway.BaseURL.RawQuery != "" || c.Gateway.BaseURL.Fragment != "" {
		return errors.New("gateway.base_url must contain only a scheme and host")
	}
	if c.Gateway.BearerTokenEnv == "" || strings.TrimSpace(c.Gateway.BearerToken) == "" {
		return errors.New("gateway bearer token is required")
	}
	if c.Gateway.ResponseHeaderTimeout <= 0 {
		return errors.New("gateway.response_header_timeout must be positive")
	}
	return nil
}

func parseDuration(name string, raw string) (time.Duration, error) {
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return value, nil
}
