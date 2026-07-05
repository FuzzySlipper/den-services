package review

import (
	"os"
	"path/filepath"
	"testing"

	sharedconfig "den-services/shared/config"
)

func TestLoadConfigPinsReviewPortAndSuccessorUpstreams(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
bind_addr: "127.0.0.1:8096"
database_url_env: "DEN_REVIEW_DATABASE_URL"
service_token_env: "DEN_REVIEW_SERVICE_TOKEN"
allow_unauthenticated_local_dev: true
projects_base_url_env: "DEN_PROJECTS_BASE_URL"
tasks_base_url_env: "DEN_TASKS_BASE_URL"
messages_base_url_env: "DEN_MESSAGES_BASE_URL"
http:
  read_header_timeout: "5s"
github:
  enabled: true
  api_base_url: "https://api.github.com"
  token_env: "DEN_REVIEW_GITHUB_TOKEN"
  poll_interval: "30s"
  request_timeout: "10s"
  default_timeout: "30m"
  max_timeout: "2h"
  batch_size: 10
  status_url_base: "http://127.0.0.1:8096"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfigFromPathWithValues(path, sharedconfig.FromMap(map[string]string{
		"DEN_REVIEW_DATABASE_URL":  "postgres://review",
		"DEN_REVIEW_SERVICE_TOKEN": "review-token",
		"DEN_PROJECTS_BASE_URL":    "http://127.0.0.1:8091",
		"DEN_TASKS_BASE_URL":       "http://127.0.0.1:8092",
		"DEN_MESSAGES_BASE_URL":    "http://127.0.0.1:8093",
	}))
	if err != nil {
		t.Fatalf("LoadConfigFromPathWithValues() error = %v", err)
	}
	if cfg.BindAddr != "127.0.0.1:8096" {
		t.Fatalf("BindAddr = %q", cfg.BindAddr)
	}
	if !cfg.AllowUnauthenticatedLocalDev {
		t.Fatal("AllowUnauthenticatedLocalDev = false, want true")
	}
	if cfg.ProjectsBaseURL != "http://127.0.0.1:8091" || cfg.TasksBaseURL != "http://127.0.0.1:8092" || cfg.MessagesBaseURL != "http://127.0.0.1:8093" {
		t.Fatalf("upstream URLs not pinned to successor services: %#v", cfg)
	}
	if cfg.ProjectsToken != "review-token" || cfg.TasksToken != "review-token" || cfg.MessagesToken != "review-token" {
		t.Fatalf("upstream tokens did not default to service token: %#v", cfg)
	}
	if !cfg.GitHub.Enabled || cfg.GitHub.APIBaseURL != "https://api.github.com" || cfg.GitHub.DefaultTimeout.String() != "30m0s" {
		t.Fatalf("github config not loaded: %#v", cfg.GitHub)
	}
}
