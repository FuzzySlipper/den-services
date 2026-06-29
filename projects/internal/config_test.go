package projects

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
bind_addr: "127.0.0.1:8091"
database_url_env: "DEN_PROJECTS_DATABASE_URL"
service_token_env: "DEN_PROJECTS_SERVICE_TOKEN"
http:
  read_header_timeout: "5s"
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := LoadConfigFromPathWithValues(path, sharedconfig.FromMap(map[string]string{
		"DEN_PROJECTS_DATABASE_URL":  "postgres://projects",
		"DEN_PROJECTS_SERVICE_TOKEN": "token",
	}))
	if err != nil {
		t.Fatalf("LoadConfigFromPathWithValues() error = %v", err)
	}
	if cfg.BindAddr != "127.0.0.1:8091" {
		t.Fatalf("BindAddr = %q", cfg.BindAddr)
	}
	if cfg.DatabaseURL != "postgres://projects" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.ServiceToken != "token" {
		t.Fatalf("ServiceToken = %q", cfg.ServiceToken)
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", cfg.HTTP.ReadHeaderTimeout)
	}
}
