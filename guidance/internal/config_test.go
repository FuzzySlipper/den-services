package guidance

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	sharedconfig "den-services/shared/config"
)

func TestLoadConfigFromPathWithValues(t *testing.T) {
	path := writeConfig(t, `bind_addr: "127.0.0.1:8097"
database_url_env: "DEN_GUIDANCE_DATABASE_URL"
projects_base_url_env: "DEN_PROJECTS_BASE_URL"
documents_base_url_env: "DEN_DOCUMENTS_BASE_URL"
knowledge_base_url_env: "DEN_KNOWLEDGE_BASE_URL"
max_packet_bytes: 1234
http:
  read_header_timeout: "5s"
`)
	values := sharedconfig.FromMap(map[string]string{
		"DEN_GUIDANCE_DATABASE_URL": "postgres://guidance",
		"DEN_PROJECTS_BASE_URL":     "http://127.0.0.1:8091",
		"DEN_DOCUMENTS_BASE_URL":    "http://127.0.0.1:8094",
		"DEN_KNOWLEDGE_BASE_URL":    "http://127.0.0.1:8095",
	})

	cfg, err := LoadConfigFromPathWithValues(path, values)
	if err != nil {
		t.Fatalf("LoadConfigFromPathWithValues() error = %v", err)
	}
	if cfg.BindAddr != "127.0.0.1:8097" {
		t.Fatalf("BindAddr = %q", cfg.BindAddr)
	}
	if cfg.MaxPacketBytes != 1234 {
		t.Fatalf("MaxPacketBytes = %d, want 1234", cfg.MaxPacketBytes)
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", cfg.HTTP.ReadHeaderTimeout)
	}
}

func TestLoadConfigFromPathWithValuesRequiresDocumentsURL(t *testing.T) {
	path := writeConfig(t, `bind_addr: "127.0.0.1:8097"
database_url_env: "DEN_GUIDANCE_DATABASE_URL"
projects_base_url_env: "DEN_PROJECTS_BASE_URL"
documents_base_url_env: "DEN_DOCUMENTS_BASE_URL"
http:
  read_header_timeout: "5s"
`)
	_, err := LoadConfigFromPathWithValues(path, sharedconfig.FromMap(map[string]string{
		"DEN_GUIDANCE_DATABASE_URL": "postgres://guidance",
		"DEN_PROJECTS_BASE_URL":     "http://127.0.0.1:8091",
	}))
	if err == nil {
		t.Fatal("LoadConfigFromPathWithValues() error = nil")
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
