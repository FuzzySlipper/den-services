package documents

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	sharedconfig "den-services/shared/config"
)

func TestLoadConfigFromPathWithValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
bind_addr: "127.0.0.1:8094"
database_url_env: "DEN_DOCUMENTS_DATABASE_URL"
projects_base_url_env: "DEN_PROJECTS_BASE_URL"
agent_guidance_base_url_env: "DEN_AGENT_GUIDANCE_BASE_URL"
http:
  read_header_timeout: "5s"
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := LoadConfigFromPathWithValues(path, sharedconfig.FromMap(map[string]string{
		"DEN_DOCUMENTS_DATABASE_URL":  "postgres://documents",
		"DEN_PROJECTS_BASE_URL":       "http://projects",
		"DEN_AGENT_GUIDANCE_BASE_URL": "http://guidance",
	}))
	if err != nil {
		t.Fatalf("LoadConfigFromPathWithValues() error = %v", err)
	}
	if cfg.BindAddr != "127.0.0.1:8094" || cfg.DatabaseURL != "postgres://documents" {
		t.Fatalf("core config = %#v", cfg)
	}
	if cfg.ProjectsBaseURL != "http://projects" {
		t.Fatalf("projects config = %#v", cfg)
	}
	if cfg.AgentGuidanceBaseURL != "http://guidance" {
		t.Fatalf("guidance config = %#v", cfg)
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", cfg.HTTP.ReadHeaderTimeout)
	}
}
