package tasks

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
bind_addr: "127.0.0.1:8092"
database_url_env: "DEN_TASKS_DATABASE_URL"
service_token_env: "DEN_TASKS_SERVICE_TOKEN"
projects_base_url_env: "DEN_PROJECTS_BASE_URL"
projects_token_env: "DEN_PROJECTS_SERVICE_TOKEN"
http:
  read_header_timeout: "5s"
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := LoadConfigFromPathWithValues(path, sharedconfig.FromMap(map[string]string{
		"DEN_TASKS_DATABASE_URL":     "postgres://tasks",
		"DEN_TASKS_SERVICE_TOKEN":    "tasks-token",
		"DEN_PROJECTS_BASE_URL":      "http://127.0.0.1:8091",
		"DEN_PROJECTS_SERVICE_TOKEN": "projects-token",
	}))
	if err != nil {
		t.Fatalf("LoadConfigFromPathWithValues() error = %v", err)
	}
	if cfg.BindAddr != "127.0.0.1:8092" {
		t.Fatalf("BindAddr = %q", cfg.BindAddr)
	}
	if cfg.DatabaseURL != "postgres://tasks" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.ServiceToken != "tasks-token" {
		t.Fatalf("ServiceToken = %q", cfg.ServiceToken)
	}
	if cfg.ProjectsBaseURL != "http://127.0.0.1:8091" || cfg.ProjectsToken != "projects-token" {
		t.Fatalf("projects config = %q %q", cfg.ProjectsBaseURL, cfg.ProjectsToken)
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", cfg.HTTP.ReadHeaderTimeout)
	}
}
