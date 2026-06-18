package gateway

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigFromPath(t *testing.T) {
	t.Setenv("DEN_GATEWAY_SERVICE_TOKEN", "secret")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
bind_addr: "127.0.0.1:8079"
routing_config_path: "config/routes.yaml"
http:
  read_header_timeout: "5s"
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("LoadConfigFromPath() error = %v", err)
	}
	if cfg.BindAddr != "127.0.0.1:8079" {
		t.Fatalf("BindAddr = %s", cfg.BindAddr)
	}
	if cfg.ServiceToken != "secret" {
		t.Fatal("ServiceToken was not loaded from env")
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", cfg.HTTP.ReadHeaderTimeout)
	}
}

func TestLoadConfigRequiresServiceToken(t *testing.T) {
	t.Setenv("DEN_GATEWAY_SERVICE_TOKEN", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
bind_addr: "127.0.0.1:8079"
routing_config_path: "config/routes.yaml"
http:
  read_header_timeout: "5s"
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadConfigFromPath(path)
	if err == nil {
		t.Fatal("LoadConfigFromPath() error = nil, want error")
	}
}
