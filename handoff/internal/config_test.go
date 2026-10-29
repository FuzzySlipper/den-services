package handoff

import (
	"os"
	"path/filepath"
	"testing"

	sharedconfig "den-services/shared/config"
)

func TestLoadConfigFromPathWithValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`bind_addr: "127.0.0.1:8099"
database_url_env: "DATABASE_URL"
http:
  read_header_timeout: "5s"
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := LoadConfigFromPathWithValues(path, sharedconfig.FromMap(map[string]string{
		"DATABASE_URL": "postgres://handoff",
	}))
	if err != nil {
		t.Fatalf("LoadConfigFromPathWithValues() error = %v", err)
	}
	if cfg.BindAddr != "127.0.0.1:8099" || cfg.DatabaseURL != "postgres://handoff" {
		t.Fatalf("config = %#v", cfg)
	}
}
