package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sharedconfig "den-services/shared/config"
)

func TestLoadConfigFromPathWithValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
bind_addr: "127.0.0.1:8092"
database_url_env: "DEN_TASKS_DATABASE_URL"
projects_base_url_env: "DEN_PROJECTS_BASE_URL"
stream:
  poll_interval: "2s"
  heartbeat_interval: "15s"
  default_limit: 100
  max_limit: 500
http:
  read_header_timeout: "5s"
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := LoadConfigFromPathWithValues(path, sharedconfig.FromMap(map[string]string{
		"DEN_TASKS_DATABASE_URL": "postgres://tasks",
		"DEN_PROJECTS_BASE_URL":  "http://127.0.0.1:8091",
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
	if cfg.ProjectsBaseURL != "http://127.0.0.1:8091" {
		t.Fatalf("ProjectsBaseURL = %q", cfg.ProjectsBaseURL)
	}
	if cfg.Stream.PollInterval != 2*time.Second || cfg.Stream.HeartbeatInterval != 15*time.Second {
		t.Fatalf("Stream intervals = %+v", cfg.Stream)
	}
	if cfg.Stream.DefaultLimit != 100 || cfg.Stream.MaxLimit != 500 {
		t.Fatalf("Stream limits = %+v", cfg.Stream)
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", cfg.HTTP.ReadHeaderTimeout)
	}
}

func TestLoadConfigRequiresProjectsBaseURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
bind_addr: "127.0.0.1:8092"
database_url_env: "DEN_TASKS_DATABASE_URL"
projects_base_url_env: "DEN_PROJECTS_BASE_URL"
stream:
  poll_interval: "2s"
  heartbeat_interval: "15s"
  default_limit: 100
  max_limit: 500
http:
  read_header_timeout: "5s"
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := LoadConfigFromPathWithValues(path, sharedconfig.FromMap(map[string]string{
		"DEN_TASKS_DATABASE_URL": "postgres://tasks",
	}))
	if err == nil || !strings.Contains(err.Error(), "projects base url is required") {
		t.Fatalf("LoadConfigFromPathWithValues() error = %v", err)
	}
}
