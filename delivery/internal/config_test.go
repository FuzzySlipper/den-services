package delivery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigLoadsRuntimeServiceURL(t *testing.T) {
	t.Setenv("DEN_DELIVERY_DATABASE_URL", "postgres://delivery.example/denservices")

	cfg, err := LoadConfigFromPath(writeTestConfig(t))
	if err != nil {
		t.Fatalf("LoadConfigFromPath() error = %v", err)
	}
	if cfg.RuntimeServiceURL != "http://127.0.0.1:8081" {
		t.Fatalf("RuntimeServiceURL = %q", cfg.RuntimeServiceURL)
	}
}

func writeTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`
bind_addr: "127.0.0.1:8083"
database_url: "${DEN_DELIVERY_DATABASE_URL}"
runtime_service_url: "http://127.0.0.1:8081"

delivery:
  default_ttl: "5m"
  max_ttl: "1h"

reaper:
  sweep_interval: "60s"
  pending_ttl: "5m"
  running_ttl: "30m"

runtime_http:
  timeout: "5s"

http:
  read_header_timeout: "5s"
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
