package webedge

import (
	"os"
	"path/filepath"
	"testing"

	sharedconfig "den-services/shared/config"
)

func TestLoadFromPathWithValues(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "web-edge.yaml")
	data := []byte(`
server:
  listen_addr: "0.0.0.0:18080"
  read_header_timeout: "5s"
  idle_timeout: "2m"
static:
  root: "/data/services/den-web/wwwroot"
  runtime_config_file: "den-web-config.json"
  build_sentinel_file: "den-web-build.json"
  immutable_asset_max_age: "8760h"
gateway:
  base_url: "http://127.0.0.1:8079"
  bearer_token_env: "EDGE_TOKEN"
  response_header_timeout: "30s"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromPathWithValues(path, sharedconfig.FromMap(map[string]string{
		"EDGE_TOKEN": "secret",
	}))
	if err != nil {
		t.Fatalf("LoadFromPathWithValues() error = %v", err)
	}
	if cfg.Gateway.BearerToken != "secret" {
		t.Fatalf("Gateway.BearerToken = %q, want secret", cfg.Gateway.BearerToken)
	}
	if cfg.Static.RuntimeConfigPath != "/data/services/den-web/wwwroot/den-web-config.json" {
		t.Fatalf("Static.RuntimeConfigPath = %q", cfg.Static.RuntimeConfigPath)
	}
}

func TestLoadFromPathWithValuesRequiresGatewayToken(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "web-edge.yaml")
	data := []byte(`
server:
  listen_addr: "127.0.0.1:18080"
  read_header_timeout: "5s"
  idle_timeout: "2m"
static:
  root: "/data/services/den-web/wwwroot"
  runtime_config_file: "den-web-config.json"
  build_sentinel_file: "den-web-build.json"
  immutable_asset_max_age: "8760h"
gateway:
  base_url: "http://127.0.0.1:8079"
  bearer_token_env: "EDGE_TOKEN"
  response_header_timeout: "30s"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFromPathWithValues(path, sharedconfig.FromMap(nil)); err == nil {
		t.Fatal("LoadFromPathWithValues() error = nil, want missing token error")
	}
}
