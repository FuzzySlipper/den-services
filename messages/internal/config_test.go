package messages

import (
	"os"
	"path/filepath"
	"testing"

	sharedconfig "den-services/shared/config"
)

func TestLoadConfigRequiresTasksAndProjectsClients(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
bind_addr: "127.0.0.1:8093"
database_url_env: "DEN_MESSAGES_DATABASE_URL"
projects_base_url_env: "DEN_PROJECTS_BASE_URL"
tasks_base_url_env: "DEN_TASKS_BASE_URL"
http:
  read_header_timeout: "5s"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	values := sharedconfig.FromMap(map[string]string{
		"DEN_MESSAGES_DATABASE_URL": "postgres://example",
		"DEN_PROJECTS_BASE_URL":     "http://projects",
		"DEN_TASKS_BASE_URL":        "http://tasks",
	})
	cfg, err := LoadConfigFromPathWithValues(path, values)
	if err != nil {
		t.Fatalf("LoadConfigFromPathWithValues() error = %v", err)
	}
	if cfg.ProjectsBaseURL != "http://projects" || cfg.TasksBaseURL != "http://tasks" {
		t.Fatalf("upstream base URLs = %#v", cfg)
	}
}
