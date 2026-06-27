package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	sharedconfig "den-services/shared/config"
)

type Config struct {
	Server   ServerConfig
	Security SecurityConfig
	Routes   RouteConfig
	Backends []BackendConfig
}

type ServerConfig struct {
	ListenAddr        string
	MCPEndpointPath   string
	ReadHeaderTimeout time.Duration
}

type SecurityConfig struct {
	ServiceTokenEnv              string
	ServiceToken                 string
	AllowUnauthenticatedLocalDev bool
}

type RouteConfig struct {
	TablePath string
}

type BackendConfig struct {
	Name            string
	BaseURL         string
	HealthPath      string
	Timeout         time.Duration
	ServiceTokenEnv string
	ServiceToken    string
}

type configFile struct {
	Server   serverConfigFile    `yaml:"server"`
	Security securityConfigFile  `yaml:"security"`
	Routes   routeConfigFile     `yaml:"routes"`
	Backends []backendConfigFile `yaml:"backends"`
}

type serverConfigFile struct {
	ListenAddr        string `yaml:"listen_addr"`
	MCPEndpointPath   string `yaml:"mcp_endpoint_path"`
	ReadHeaderTimeout string `yaml:"read_header_timeout"`
}

type securityConfigFile struct {
	ServiceTokenEnv              string `yaml:"service_token_env"`
	AllowUnauthenticatedLocalDev bool   `yaml:"allow_unauthenticated_local_dev"`
}

type routeConfigFile struct {
	TablePath string `yaml:"table_path"`
}

type backendConfigFile struct {
	Name            string `yaml:"name"`
	BaseURL         string `yaml:"base_url"`
	HealthPath      string `yaml:"health_path"`
	Timeout         string `yaml:"timeout"`
	ServiceTokenEnv string `yaml:"service_token_env"`
}

func Load() (*Config, error) {
	return LoadFromPath(configPath())
}

func LoadFromPath(path string) (*Config, error) {
	values, err := sharedconfig.Load()
	if err != nil {
		return nil, err
	}
	return LoadFromPathWithValues(path, values)
}

func LoadFromPathWithValues(path string, values sharedconfig.Values) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading mcp config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing mcp config %s: %w", path, err)
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

func (c configFile) toConfig(values sharedconfig.Values) (*Config, error) {
	serverConfig, err := c.Server.toConfig()
	if err != nil {
		return nil, err
	}
	backends, err := c.toBackendConfigs(values)
	if err != nil {
		return nil, err
	}
	serviceTokenEnv := strings.TrimSpace(c.Security.ServiceTokenEnv)
	return &Config{
		Server: serverConfig,
		Security: SecurityConfig{
			ServiceTokenEnv:              serviceTokenEnv,
			ServiceToken:                 values.String(serviceTokenEnv, ""),
			AllowUnauthenticatedLocalDev: c.Security.AllowUnauthenticatedLocalDev,
		},
		Routes: RouteConfig{
			TablePath: strings.TrimSpace(c.Routes.TablePath),
		},
		Backends: backends,
	}, nil
}

func (c configFile) toBackendConfigs(values sharedconfig.Values) ([]BackendConfig, error) {
	backends := make([]BackendConfig, 0, len(c.Backends))
	for index, backend := range c.Backends {
		parsed, err := backend.toConfig(values)
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
		ListenAddr:        strings.TrimSpace(c.ListenAddr),
		MCPEndpointPath:   cleanPath(c.MCPEndpointPath),
		ReadHeaderTimeout: readHeaderTimeout,
	}, nil
}

func (c backendConfigFile) toConfig(values sharedconfig.Values) (BackendConfig, error) {
	timeout, err := parseRequiredDuration("backend.timeout", c.Timeout)
	if err != nil {
		return BackendConfig{}, err
	}
	serviceTokenEnv := strings.TrimSpace(c.ServiceTokenEnv)
	return BackendConfig{
		Name:            strings.TrimSpace(c.Name),
		BaseURL:         strings.TrimRight(strings.TrimSpace(c.BaseURL), "/"),
		HealthPath:      cleanPath(c.HealthPath),
		Timeout:         timeout,
		ServiceTokenEnv: serviceTokenEnv,
		ServiceToken:    values.String(serviceTokenEnv, ""),
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
	if c.Security.ServiceTokenEnv == "" {
		return errors.New("security.service_token_env is required")
	}
	if !c.Security.AllowUnauthenticatedLocalDev && c.Security.ServiceToken == "" {
		return fmt.Errorf("%s is required when unauthenticated local dev is disabled", c.Security.ServiceTokenEnv)
	}
	if c.Routes.TablePath == "" {
		return errors.New("routes.table_path is required")
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
