package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	sharedconfig "den-services/shared/config"
)

func TestLoadFromPathWithValues(t *testing.T) {
	path := writeConfig(t, validConfig())
	values := sharedconfig.FromMap(map[string]string{
		"DEN_VISUAL_INSPECT_TOKEN":        "local-token",
		"DEN_VISUAL_INSPECT_LLM_BASE_URL": "http://127.0.0.1:11434/v1",
		"DEN_VISUAL_INSPECT_LLM_API_KEY":  "local-key",
	})

	cfg, err := LoadFromPathWithValues(path, values)
	if err != nil {
		t.Fatalf("LoadFromPathWithValues() error = %v", err)
	}
	if cfg.Server.ListenAddr != "127.0.0.1:18140" {
		t.Fatalf("Server.ListenAddr = %q", cfg.Server.ListenAddr)
	}
	if cfg.Server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("Server.ReadHeaderTimeout = %s", cfg.Server.ReadHeaderTimeout)
	}
	if cfg.Security.ServiceToken != "local-token" {
		t.Fatalf("Security.ServiceToken = %q", cfg.Security.ServiceToken)
	}
	if cfg.LLM.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Fatalf("LLM.BaseURL = %q", cfg.LLM.BaseURL)
	}
}

func TestLoadFromPathWithValuesRejectsMissingDefaultProfile(t *testing.T) {
	path := writeConfig(t, validConfigWith(`default_profile: "missing"`))

	_, err := LoadFromPathWithValues(path, sharedconfig.FromMap(nil))
	if err == nil {
		t.Fatal("LoadFromPathWithValues() error = nil")
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

func validConfig() string {
	return validConfigWith(`default_profile: "visual-inspect-v0"`)
}

func validConfigWith(defaultProfile string) string {
	return `server:
  listen_addr: "127.0.0.1:18140"
  read_header_timeout: "5s"
security:
  service_token_env: "DEN_VISUAL_INSPECT_TOKEN"
artifacts:
  max_images: 4
  max_bytes_per_image: 8000000
  max_pixels_per_image: 6000000
  allowed_schemes: ["file", "http", "https", "den-artifact"]
  allowed_file_roots: ["/var/lib/den/artifacts", "/tmp/den-visual-inspect"]
llm:
  provider: "openai_compatible"
  base_url_env: "DEN_VISUAL_INSPECT_LLM_BASE_URL"
  api_key_env: "DEN_VISUAL_INSPECT_LLM_API_KEY"
  model: "qwen-vl-or-other"
  temperature: 0
  timeout: "45s"
  max_output_tokens: 2000
prompts:
  ` + defaultProfile + `
  profiles:
    visual-inspect-v0:
      system_prompt_file: "prompts/visual-inspect.system.md"
      developer_prompt_file: "prompts/visual-inspect.developer.md"
      response_schema_file: "schemas/evaluate-response.schema.json"
      min_confidence_for_pass: 0.70
      min_confidence_for_fail: 0.60
`
}
