package artifacts

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	sharedconfig "den-services/shared/config"
)

func TestLoadConfigFromPathWithValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(validConfig()), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	values := sharedconfig.FromMap(map[string]string{
		"DEN_ARTIFACTS_DATABASE_URL":  "postgres://example",
		"DEN_ARTIFACTS_SERVICE_TOKEN": "token",
	})

	cfg, err := LoadConfigFromPathWithValues(path, values)
	if err != nil {
		t.Fatalf("LoadConfigFromPathWithValues() error = %v", err)
	}
	if cfg.BindAddr != "127.0.0.1:8090" {
		t.Fatalf("BindAddr = %q", cfg.BindAddr)
	}
	if cfg.Storage.RootPath != "/var/lib/den/artifacts" {
		t.Fatalf("Storage.RootPath = %q", cfg.Storage.RootPath)
	}
	if cfg.Retention.TemporaryTTL != 168*time.Hour {
		t.Fatalf("TemporaryTTL = %s", cfg.Retention.TemporaryTTL)
	}
}

func validConfig() string {
	return `bind_addr: "127.0.0.1:8090"
database_url_env: "DEN_ARTIFACTS_DATABASE_URL"
service_token_env: "DEN_ARTIFACTS_SERVICE_TOKEN"
storage:
  backend: "filesystem"
  root_path: "/var/lib/den/artifacts"
  key_prefix: "sha256"
limits:
  max_bytes_per_artifact: 25000000
  max_pixels_per_image: 24000000
retention:
  default_ttl: "0s"
  temporary_ttl: "168h"
http:
  read_header_timeout: "5s"
`
}
