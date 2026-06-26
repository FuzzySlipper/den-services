package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	sharedconfig "den-services/shared/config"
)

func TestLoadFromPathWithValues(t *testing.T) {
	path := writeConfig(t, validConfig())
	values := sharedconfig.FromMap(map[string]string{
		"DEN_MCP_SERVICE_TOKEN":  "mcp-token",
		"DEN_CORE_SERVICE_TOKEN": "core-token",
	})

	cfg, err := LoadFromPathWithValues(path, values)
	if err != nil {
		t.Fatalf("LoadFromPathWithValues() error = %v", err)
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
	if cfg.Security.ServiceToken != "mcp-token" {
		t.Fatalf("Security.ServiceToken = %q", cfg.Security.ServiceToken)
	}
	if cfg.Backends[0].ServiceToken != "core-token" {
		t.Fatalf("Backends[0].ServiceToken = %q", cfg.Backends[0].ServiceToken)
	}
	if cfg.Backends[0].Timeout != 3*time.Second {
		t.Fatalf("Backends[0].Timeout = %s", cfg.Backends[0].Timeout)
	}
}

func TestLoadFromPathWithValuesRequiresTokenWhenAuthEnabled(t *testing.T) {
	path := writeConfig(t, validConfigWithAuth(false))

	_, err := LoadFromPathWithValues(path, sharedconfig.FromMap(nil))
	if err == nil {
		t.Fatal("LoadFromPathWithValues() error = nil")
	}
}

func TestLoadFromPathWithValuesRejectsDuplicateBackendNames(t *testing.T) {
	path := writeConfig(t, validConfig()+`
  - name: "den-core"
    base_url: "http://127.0.0.1:5298"
    health_path: "/health"
    timeout: "3s"
`)

	_, err := LoadFromPathWithValues(path, sharedconfig.FromMap(map[string]string{
		"DEN_MCP_SERVICE_TOKEN": "mcp-token",
	}))
	if err == nil {
		t.Fatal("LoadFromPathWithValues() error = nil")
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
	return validConfigWithAuth(true)
}

func validConfigWithAuth(allowUnauthenticatedLocalDev bool) string {
	return `server:
  listen_addr: "127.0.0.1:18090"
  mcp_endpoint_path: "mcp"
  read_header_timeout: "5s"
security:
  service_token_env: "DEN_MCP_SERVICE_TOKEN"
  allow_unauthenticated_local_dev: ` + boolString(allowUnauthenticatedLocalDev) + `
routes:
  table_path: "routes.example.yaml"
backends:
  - name: "den-core"
    base_url: "http://127.0.0.1:5299/"
    health_path: "health"
    timeout: "3s"
    service_token_env: "DEN_CORE_SERVICE_TOKEN"
`
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
