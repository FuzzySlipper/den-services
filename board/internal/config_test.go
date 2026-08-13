package board

import (
	"os"
	"path/filepath"
	"testing"

	sharedconfig "den-services/shared/config"
)

func TestLoadConfigReadsBoardBoundsAndAdapterIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := `
bind_addr: "127.0.0.1:8100"
database_url_env: "DEN_BOARD_DATABASE_URL"
service_token_env: "DEN_BOARD_SERVICE_TOKEN"
adapter_identity: "configured-den-web"
projects_base_url_env: "DEN_PROJECTS_BASE_URL"
projects_token_env: "DEN_PROJECTS_SERVICE_TOKEN"
projects_request_timeout: "3s"
limits:
  default_page_size: 10
  max_page_size: 20
  max_path_comments: 30
  max_project_id_bytes: 31
  max_title_bytes: 32
  max_body_bytes: 33
  max_author_identity_bytes: 34
  max_metadata_bytes: 35
  max_search_query_bytes: 36
  max_purge_reason_bytes: 37
http:
  read_header_timeout: "4s"
  max_request_body_bytes: 4096
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFromPathWithValues(path, sharedconfig.FromMap(map[string]string{
		"DEN_BOARD_DATABASE_URL":     "postgres://board",
		"DEN_BOARD_SERVICE_TOKEN":    "board-token",
		"DEN_PROJECTS_BASE_URL":      "http://projects",
		"DEN_PROJECTS_SERVICE_TOKEN": "projects-token",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdapterIdentity != "configured-den-web" {
		t.Fatalf("adapter identity = %q", cfg.AdapterIdentity)
	}
	if cfg.HTTP.MaxRequestBodyBytes != 4096 {
		t.Fatalf("max request body bytes = %d", cfg.HTTP.MaxRequestBodyBytes)
	}
	if cfg.Limits.MaxTitleBytes != 32 || cfg.Limits.MaxBodyBytes != 33 || cfg.Limits.MaxPurgeReasonBytes != 37 {
		t.Fatalf("limits = %#v", cfg.Limits)
	}
}

func TestConfigDefaultsNewBoundsForExistingYAML(t *testing.T) {
	cfg, err := (configFile{
		BindAddr:               "127.0.0.1:8100",
		DatabaseURLEnv:         "DEN_BOARD_DATABASE_URL",
		ServiceTokenEnv:        "DEN_BOARD_SERVICE_TOKEN",
		ProjectsBaseURLEnv:     "DEN_PROJECTS_BASE_URL",
		ProjectsTokenEnv:       "DEN_PROJECTS_SERVICE_TOKEN",
		ProjectsRequestTimeout: "5s",
		Limits: limitsFile{
			DefaultPageSize: DefaultPageSize,
			MaxPageSize:     MaxPageSize,
			MaxPathComments: MaxPathComments,
		},
		HTTP: httpConfigFile{ReadHeaderTimeout: "5s"},
	}).toConfig(sharedconfig.FromMap(map[string]string{
		"DEN_BOARD_DATABASE_URL":     "postgres://board",
		"DEN_BOARD_SERVICE_TOKEN":    "board-token",
		"DEN_PROJECTS_BASE_URL":      "http://projects",
		"DEN_PROJECTS_SERVICE_TOKEN": "projects-token",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdapterIdentity != DefaultAdapterIdentity || cfg.HTTP.MaxRequestBodyBytes != DefaultMaxRequestBodyBytes {
		t.Fatalf("defaults = adapter %q, body %d", cfg.AdapterIdentity, cfg.HTTP.MaxRequestBodyBytes)
	}
	if cfg.Limits.MaxTitleBytes != DefaultMaxTitleBytes || cfg.Limits.MaxBodyBytes != DefaultMaxBodyBytes {
		t.Fatalf("default field limits = %#v", cfg.Limits)
	}
}
