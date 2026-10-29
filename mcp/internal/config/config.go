package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultDetailReferenceTTL = 15 * time.Minute

type Config struct {
	Server   ServerConfig
	Routes   RouteConfig
	Details  DetailConfig
	Backends []BackendConfig
}

type ServerConfig struct {
	ListenAddr         string
	MCPEndpointPath    string
	ReadHeaderTimeout  time.Duration
	DefaultToolProfile string
}

type RouteConfig struct {
	TablePath string
}

type DetailConfig struct {
	ReferenceTTL time.Duration
}

type BackendConfig struct {
	Name       string
	BaseURL    string
	HealthPath string
	Timeout    time.Duration
}

type configFile struct {
	Server   serverConfigFile    `yaml:"server"`
	Routes   routeConfigFile     `yaml:"routes"`
	Details  detailConfigFile    `yaml:"details"`
	Backends []backendConfigFile `yaml:"backends"`
}

type serverConfigFile struct {
	ListenAddr         string `yaml:"listen_addr"`
	MCPEndpointPath    string `yaml:"mcp_endpoint_path"`
	ReadHeaderTimeout  string `yaml:"read_header_timeout"`
	DefaultToolProfile string `yaml:"default_tool_profile"`
}

type routeConfigFile struct {
	TablePath string `yaml:"table_path"`
}

type detailConfigFile struct {
	ReferenceTTL string `yaml:"reference_ttl"`
}

type backendConfigFile struct {
	Name       string `yaml:"name"`
	BaseURL    string `yaml:"base_url"`
	HealthPath string `yaml:"health_path"`
	Timeout    string `yaml:"timeout"`
}

func Load() (*Config, error) {
	return LoadFromPath(configPath())
}

func LoadFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading mcp config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing mcp config %s: %w", path, err)
	}
	cfg, err := file.toConfig()
	if err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c configFile) toConfig() (*Config, error) {
	serverConfig, err := c.Server.toConfig()
	if err != nil {
		return nil, err
	}
	backends, err := c.toBackendConfigs()
	if err != nil {
		return nil, err
	}
	referenceTTL := defaultDetailReferenceTTL
	if strings.TrimSpace(c.Details.ReferenceTTL) != "" {
		referenceTTL, err = parseRequiredDuration("details.reference_ttl", c.Details.ReferenceTTL)
		if err != nil {
			return nil, err
		}
	}
	return &Config{
		Server: serverConfig,
		Routes: RouteConfig{
			TablePath: strings.TrimSpace(c.Routes.TablePath),
		},
		Details:  DetailConfig{ReferenceTTL: referenceTTL},
		Backends: backends,
	}, nil
}

func (c configFile) toBackendConfigs() ([]BackendConfig, error) {
	backends := make([]BackendConfig, 0, len(c.Backends))
	for index, backend := range c.Backends {
		parsed, err := backend.toConfig()
		if err != nil {
			return nil, fmt.Errorf("backends[%d]: %w", index, err)
		}
		backends = append(backends, parsed)
	}
	return backends, nil
}

func (c serverConfigFile) toConfig() (ServerConfig, error) {
	readHeaderTimeout, err := parseRequiredDuration("server.read_header_timeout", c.ReadHeaderTimeout)
	if err != nil {
		return ServerConfig{}, err
	}
	return ServerConfig{
		ListenAddr:         strings.TrimSpace(c.ListenAddr),
		MCPEndpointPath:    cleanPath(c.MCPEndpointPath),
		ReadHeaderTimeout:  readHeaderTimeout,
		DefaultToolProfile: strings.TrimSpace(c.DefaultToolProfile),
	}, nil
}

func (c backendConfigFile) toConfig() (BackendConfig, error) {
	timeout, err := parseRequiredDuration("backend.timeout", c.Timeout)
	if err != nil {
		return BackendConfig{}, err
	}
	return BackendConfig{
		Name:       strings.TrimSpace(c.Name),
		BaseURL:    strings.TrimRight(strings.TrimSpace(c.BaseURL), "/"),
		HealthPath: cleanPath(c.HealthPath),
		Timeout:    timeout,
	}, nil
}

func (c *Config) validate() error {
	if c.Server.ListenAddr == "" {
		return errors.New("server.listen_addr is required")
	}
	if c.Server.MCPEndpointPath == "" {
		return errors.New("server.mcp_endpoint_path is required")
	}
	if c.Server.ReadHeaderTimeout <= 0 {
		return errors.New("server.read_header_timeout must be positive")
	}
	if c.Routes.TablePath == "" {
		return errors.New("routes.table_path is required")
	}
	if c.Details.ReferenceTTL <= 0 {
		return errors.New("details.reference_ttl must be positive")
	}
	if len(c.Backends) == 0 {
		return errors.New("at least one backend is required")
	}
	return validateBackends(c.Backends)
}

func validateBackends(backends []BackendConfig) error {
	seen := make(map[string]bool, len(backends))
	for _, backend := range backends {
		if err := backend.validate(); err != nil {
			return err
		}
		if seen[backend.Name] {
			return fmt.Errorf("duplicate backend name %q", backend.Name)
		}
		seen[backend.Name] = true
	}
	return nil
}

func (b BackendConfig) validate() error {
	if b.Name == "" {
		return errors.New("backend.name is required")
	}
	parsedURL, err := url.Parse(b.BaseURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("backend %s base_url must be an absolute URL", b.Name)
	}
	if b.HealthPath == "" {
		return fmt.Errorf("backend %s health_path is required", b.Name)
	}
	if b.Timeout <= 0 {
		return fmt.Errorf("backend %s timeout must be positive", b.Name)
	}
	return nil
}

func parseRequiredDuration(name string, raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", name, err)
	}
	return parsed, nil
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func configPath() string {
	path := os.Getenv("MCP_CONFIG_PATH")
	if path == "" {
		return "config.example.yaml"
	}
	return path
}
