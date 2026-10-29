package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadFromPath(t *testing.T) {
	path := writeConfig(t, validConfig())

	cfg, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if cfg.Server.ListenAddr != "127.0.0.1:18090" {
		t.Fatalf("Server.ListenAddr = %q", cfg.Server.ListenAddr)
	}
	if cfg.Server.MCPEndpointPath != "/mcp" {
		t.Fatalf("Server.MCPEndpointPath = %q", cfg.Server.MCPEndpointPath)
	}
	if cfg.Server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("Server.ReadHeaderTimeout = %s", cfg.Server.ReadHeaderTimeout)
	}
	if cfg.Server.DefaultToolProfile != "managed-runtime" {
		t.Fatalf("Server.DefaultToolProfile = %q", cfg.Server.DefaultToolProfile)
	}
	if cfg.Details.ReferenceTTL != 15*time.Minute {
		t.Fatalf("Details.ReferenceTTL = %s", cfg.Details.ReferenceTTL)
	}
	if cfg.Backends[0].Timeout != 3*time.Second {
		t.Fatalf("Backends[0].Timeout = %s", cfg.Backends[0].Timeout)
	}
}

func TestLoadFromPathRejectsDuplicateBackendNames(t *testing.T) {
	path := writeConfig(t, validConfig()+`
  - name: "den-core"
    base_url: "http://127.0.0.1:5298"
    health_path: "/health"
    timeout: "3s"
`)

	_, err := LoadFromPath(path)
	if err == nil {
		t.Fatal("LoadFromPath() error = nil")
	}
}

func TestLoadFromPathRequiresRouteTablePath(t *testing.T) {
	path := writeConfig(t, `server:
  listen_addr: "127.0.0.1:18090"
  mcp_endpoint_path: "mcp"
  read_header_timeout: "5s"
routes:
  table_path: ""
details:
  reference_ttl: "15m"
backends:
  - name: "den-core"
    base_url: "http://127.0.0.1:5299/"
    health_path: "health"
    timeout: "3s"
`)

	_, err := LoadFromPath(path)
	if err == nil {
		t.Fatal("LoadFromPath() error = nil")
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

func validConfig() string {
	return `server:
  listen_addr: "127.0.0.1:18090"
  mcp_endpoint_path: "mcp"
  read_header_timeout: "5s"
  default_tool_profile: "managed-runtime"
routes:
  table_path: "routes.example.yaml"
details:
  reference_ttl: "15m"
backends:
  - name: "den-core"
    base_url: "http://127.0.0.1:5299/"
    health_path: "health"
    timeout: "3s"
`
}
