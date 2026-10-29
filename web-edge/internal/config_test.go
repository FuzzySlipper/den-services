package webedge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromPath(t *testing.T) {
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
  response_header_timeout: "30s"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if cfg.Static.RuntimeConfigPath != "/data/services/den-web/wwwroot/den-web-config.json" {
		t.Fatalf("Static.RuntimeConfigPath = %q", cfg.Static.RuntimeConfigPath)
	}
}
