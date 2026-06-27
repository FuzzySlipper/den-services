package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	sharedconfig "den-services/shared/config"
)

const ProviderOpenAICompatible = "openai_compatible"

type Config struct {
	Server    ServerConfig
	Security  SecurityConfig
	Artifacts ArtifactConfig
	LLM       LLMConfig
	Prompts   PromptConfig
}

type ServerConfig struct {
	ListenAddr        string
	ReadHeaderTimeout time.Duration
}

type SecurityConfig struct {
	ServiceTokenEnv              string
	ServiceToken                 string
	AllowUnauthenticatedLocalDev bool
}

type ArtifactConfig struct {
	MaxImages         int
	MaxBytesPerImage  int64
	MaxPixelsPerImage int64
	AllowedSchemes    []string
	AllowedFileRoots  []string
	ServiceBaseURLEnv string
	ServiceBaseURL    string
	ServiceTokenEnv   string
	ServiceToken      string
	ServiceTimeout    time.Duration
}

type LLMConfig struct {
	Provider        string
	BaseURLEnv      string
	BaseURL         string
	APIKeyEnv       string
	APIKey          string
	Model           string
	Temperature     float64
	Timeout         time.Duration
	MaxOutputTokens int
	MaxRetries      int
}

type PromptConfig struct {
	DefaultProfile string
	Profiles       map[string]PromptProfile
}

type PromptProfile struct {
	SystemPromptFile     string
	DeveloperPromptFile  string
	ResponseSchemaFile   string
	MinConfidenceForPass float64
	MinConfidenceForFail float64
}

type configFile struct {
	Server    serverConfigFile   `yaml:"server"`
	Security  securityConfigFile `yaml:"security"`
	Artifacts artifactConfigFile `yaml:"artifacts"`
	LLM       llmConfigFile      `yaml:"llm"`
	Prompts   promptConfigFile   `yaml:"prompts"`
}

type serverConfigFile struct {
	ListenAddr        string `yaml:"listen_addr"`
	ReadHeaderTimeout string `yaml:"read_header_timeout"`
}

type securityConfigFile struct {
	ServiceTokenEnv              string `yaml:"service_token_env"`
	AllowUnauthenticatedLocalDev bool   `yaml:"allow_unauthenticated_local_dev"`
}

type artifactConfigFile struct {
	MaxImages         int                       `yaml:"max_images"`
	MaxBytesPerImage  int64                     `yaml:"max_bytes_per_image"`
	MaxPixelsPerImage int64                     `yaml:"max_pixels_per_image"`
	AllowedSchemes    []string                  `yaml:"allowed_schemes"`
	AllowedFileRoots  []string                  `yaml:"allowed_file_roots"`
	ArtifactService   artifactServiceConfigFile `yaml:"artifact_service"`
}

type artifactServiceConfigFile struct {
	BaseURLEnv      string `yaml:"base_url_env"`
	ServiceTokenEnv string `yaml:"service_token_env"`
	Timeout         string `yaml:"timeout"`
}

type llmConfigFile struct {
	Provider        string  `yaml:"provider"`
	BaseURLEnv      string  `yaml:"base_url_env"`
	APIKeyEnv       string  `yaml:"api_key_env"`
	Model           string  `yaml:"model"`
	Temperature     float64 `yaml:"temperature"`
	Timeout         string  `yaml:"timeout"`
	MaxOutputTokens int     `yaml:"max_output_tokens"`
	MaxRetries      int     `yaml:"max_retries"`
}

type promptConfigFile struct {
	DefaultProfile string                       `yaml:"default_profile"`
	Profiles       map[string]promptProfileFile `yaml:"profiles"`
}

type promptProfileFile struct {
	SystemPromptFile     string  `yaml:"system_prompt_file"`
	DeveloperPromptFile  string  `yaml:"developer_prompt_file"`
	ResponseSchemaFile   string  `yaml:"response_schema_file"`
	MinConfidenceForPass float64 `yaml:"min_confidence_for_pass"`
	MinConfidenceForFail float64 `yaml:"min_confidence_for_fail"`
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
		return nil, fmt.Errorf("reading visual-inspect config %s: %w", path, err)
	}
	var file configFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing visual-inspect config %s: %w", path, err)
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
	llmConfig, err := c.LLM.toConfig(values)
	if err != nil {
		return nil, err
	}
	return &Config{
		Server: serverConfig,
		Security: SecurityConfig{
			ServiceTokenEnv:              strings.TrimSpace(c.Security.ServiceTokenEnv),
			ServiceToken:                 values.String(strings.TrimSpace(c.Security.ServiceTokenEnv), ""),
			AllowUnauthenticatedLocalDev: c.Security.AllowUnauthenticatedLocalDev,
		},
		Artifacts: c.Artifacts.toConfig(values),
		LLM:       llmConfig,
		Prompts:   c.Prompts.toConfig(),
	}, nil
}

func (c serverConfigFile) toConfig() (ServerConfig, error) {
	readHeaderTimeout, err := parseRequiredDuration("server.read_header_timeout", c.ReadHeaderTimeout)
	if err != nil {
		return ServerConfig{}, err
	}
	return ServerConfig{
		ListenAddr:        strings.TrimSpace(c.ListenAddr),
		ReadHeaderTimeout: readHeaderTimeout,
	}, nil
}

func (c artifactConfigFile) toConfig(values sharedconfig.Values) ArtifactConfig {
	serviceTimeout, _ := time.ParseDuration(strings.TrimSpace(c.ArtifactService.Timeout))
	baseURLEnv := strings.TrimSpace(c.ArtifactService.BaseURLEnv)
	tokenEnv := strings.TrimSpace(c.ArtifactService.ServiceTokenEnv)
	return ArtifactConfig{
		MaxImages:         c.MaxImages,
		MaxBytesPerImage:  c.MaxBytesPerImage,
		MaxPixelsPerImage: c.MaxPixelsPerImage,
		AllowedSchemes:    trimmedStrings(c.AllowedSchemes),
		AllowedFileRoots:  cleanedRoots(c.AllowedFileRoots),
		ServiceBaseURLEnv: baseURLEnv,
		ServiceBaseURL:    strings.TrimRight(values.String(baseURLEnv, ""), "/"),
		ServiceTokenEnv:   tokenEnv,
		ServiceToken:      values.String(tokenEnv, ""),
		ServiceTimeout:    serviceTimeout,
	}
}

func (c llmConfigFile) toConfig(values sharedconfig.Values) (LLMConfig, error) {
	timeout, err := parseRequiredDuration("llm.timeout", c.Timeout)
	if err != nil {
		return LLMConfig{}, err
	}
	baseURLEnv := strings.TrimSpace(c.BaseURLEnv)
	apiKeyEnv := strings.TrimSpace(c.APIKeyEnv)
	return LLMConfig{
		Provider:        strings.TrimSpace(c.Provider),
		BaseURLEnv:      baseURLEnv,
		BaseURL:         values.String(baseURLEnv, ""),
		APIKeyEnv:       apiKeyEnv,
		APIKey:          values.String(apiKeyEnv, ""),
		Model:           strings.TrimSpace(c.Model),
		Temperature:     c.Temperature,
		Timeout:         timeout,
		MaxOutputTokens: c.MaxOutputTokens,
		MaxRetries:      c.MaxRetries,
	}, nil
}

func (c promptConfigFile) toConfig() PromptConfig {
	profiles := make(map[string]PromptProfile, len(c.Profiles))
	for name, profile := range c.Profiles {
		profiles[strings.TrimSpace(name)] = profile.toConfig()
	}
	return PromptConfig{
		DefaultProfile: strings.TrimSpace(c.DefaultProfile),
		Profiles:       profiles,
	}
}

func (c promptProfileFile) toConfig() PromptProfile {
	return PromptProfile{
		SystemPromptFile:     strings.TrimSpace(c.SystemPromptFile),
		DeveloperPromptFile:  strings.TrimSpace(c.DeveloperPromptFile),
		ResponseSchemaFile:   strings.TrimSpace(c.ResponseSchemaFile),
		MinConfidenceForPass: c.MinConfidenceForPass,
		MinConfidenceForFail: c.MinConfidenceForFail,
	}
}

func (c *Config) validate() error {
	if c.Server.ListenAddr == "" {
		return errors.New("server.listen_addr is required")
	}
	if c.Server.ReadHeaderTimeout <= 0 {
		return errors.New("server.read_header_timeout must be positive")
	}
	if c.Security.ServiceTokenEnv == "" {
		return errors.New("security.service_token_env is required")
	}
	if err := c.Artifacts.validate(); err != nil {
		return err
	}
	if err := c.LLM.validate(); err != nil {
		return err
	}
	return c.Prompts.validate()
}

func (c ArtifactConfig) validate() error {
	if c.MaxImages <= 0 {
		return errors.New("artifacts.max_images must be positive")
	}
	if c.MaxBytesPerImage <= 0 {
		return errors.New("artifacts.max_bytes_per_image must be positive")
	}
	if c.MaxPixelsPerImage <= 0 {
		return errors.New("artifacts.max_pixels_per_image must be positive")
	}
	if len(c.AllowedSchemes) == 0 {
		return errors.New("artifacts.allowed_schemes is required")
	}
	for _, scheme := range c.AllowedSchemes {
		if scheme == "" {
			return errors.New("artifacts.allowed_schemes cannot contain blank values")
		}
	}
	for _, root := range c.AllowedFileRoots {
		if !filepath.IsAbs(root) {
			return fmt.Errorf("artifacts.allowed_file_roots must be absolute: %s", root)
		}
	}
	if c.schemeAllowed("den-artifact") {
		if c.ServiceBaseURLEnv == "" {
			return errors.New("artifacts.artifact_service.base_url_env is required when den-artifact refs are allowed")
		}
		if c.ServiceBaseURL == "" {
			return errors.New("artifacts artifact service base url is required when den-artifact refs are allowed")
		}
		if c.ServiceTokenEnv == "" {
			return errors.New("artifacts.artifact_service.service_token_env is required when den-artifact refs are allowed")
		}
		if c.ServiceToken == "" {
			return errors.New("artifacts artifact service token is required when den-artifact refs are allowed")
		}
		if c.ServiceTimeout <= 0 {
			return errors.New("artifacts.artifact_service.timeout must be positive when den-artifact refs are allowed")
		}
	}
	return nil
}

func (c ArtifactConfig) schemeAllowed(scheme string) bool {
	for _, allowed := range c.AllowedSchemes {
		if strings.EqualFold(allowed, scheme) {
			return true
		}
	}
	return false
}

func (c LLMConfig) validate() error {
	if c.Provider != ProviderOpenAICompatible {
		return fmt.Errorf("llm.provider must be %s", ProviderOpenAICompatible)
	}
	if c.BaseURLEnv == "" {
		return errors.New("llm.base_url_env is required")
	}
	if c.APIKeyEnv == "" {
		return errors.New("llm.api_key_env is required")
	}
	if c.Model == "" {
		return errors.New("llm.model is required")
	}
	if c.Temperature < 0 {
		return errors.New("llm.temperature must be non-negative")
	}
	if c.Timeout <= 0 {
		return errors.New("llm.timeout must be positive")
	}
	if c.MaxOutputTokens <= 0 {
		return errors.New("llm.max_output_tokens must be positive")
	}
	if c.MaxRetries < 0 {
		return errors.New("llm.max_retries must be non-negative")
	}
	return nil
}

func (c PromptConfig) validate() error {
	if c.DefaultProfile == "" {
		return errors.New("prompts.default_profile is required")
	}
	if len(c.Profiles) == 0 {
		return errors.New("prompts.profiles is required")
	}
	profile, ok := c.Profiles[c.DefaultProfile]
	if !ok {
		return fmt.Errorf("prompts.default_profile not found: %s", c.DefaultProfile)
	}
	if err := profile.validate(c.DefaultProfile); err != nil {
		return err
	}
	for name, candidate := range c.Profiles {
		if strings.TrimSpace(name) == "" {
			return errors.New("prompts.profiles cannot contain a blank profile name")
		}
		if err := candidate.validate(name); err != nil {
			return err
		}
	}
	return nil
}

func (c PromptProfile) validate(name string) error {
	prefix := "prompts.profiles." + name
	if c.SystemPromptFile == "" {
		return errors.New(prefix + ".system_prompt_file is required")
	}
	if c.DeveloperPromptFile == "" {
		return errors.New(prefix + ".developer_prompt_file is required")
	}
	if c.ResponseSchemaFile == "" {
		return errors.New(prefix + ".response_schema_file is required")
	}
	if c.MinConfidenceForPass < 0 || c.MinConfidenceForPass > 1 {
		return errors.New(prefix + ".min_confidence_for_pass must be between 0 and 1")
	}
	if c.MinConfidenceForFail < 0 || c.MinConfidenceForFail > 1 {
		return errors.New(prefix + ".min_confidence_for_fail must be between 0 and 1")
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

func trimmedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, strings.TrimSpace(value))
	}
	return result
}

func cleanedRoots(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			result = append(result, trimmed)
			continue
		}
		result = append(result, filepath.Clean(trimmed))
	}
	return result
}

func configPath() string {
	path := os.Getenv("VISUAL_INSPECT_CONFIG_PATH")
	if path == "" {
		return "config.yaml"
	}
	return path
}
