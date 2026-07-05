package conversation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigFromPath(t *testing.T) {
	t.Setenv("DEN_CHANNELS_DATABASE_URL", "postgres://channels.example/denservices")
	t.Setenv("DEN_CONVERSATION_SERVICE_TOKEN", "conversation-token")
	t.Setenv("DEN_CONVERSATION_RUNTIME_SERVICE_TOKEN", "runtime-token")

	cfg, err := LoadConfigFromPath(writeTestConfig(t))
	if err != nil {
		t.Fatalf("LoadConfigFromPath() error = %v", err)
	}
	if cfg.BindAddr != "127.0.0.1:8084" {
		t.Fatalf("BindAddr = %s", cfg.BindAddr)
	}
	if cfg.DatabaseURL != "postgres://channels.example/denservices" {
		t.Fatalf("DatabaseURL = %s", cfg.DatabaseURL)
	}
	if cfg.ServiceToken != "conversation-token" {
		t.Fatalf("ServiceToken = %s", cfg.ServiceToken)
	}
	if cfg.DefaultLimit != 100 || cfg.MaxLimit != 500 {
		t.Fatalf("limits = %d/%d", cfg.DefaultLimit, cfg.MaxLimit)
	}
	if !cfg.WakeTargets.Enabled {
		t.Fatal("WakeTargets.Enabled = false, want true")
	}
	if cfg.WakeTargets.RuntimeBaseURL != "http://127.0.0.1:8081" || cfg.WakeTargets.RuntimeServiceToken != "runtime-token" || cfg.WakeTargets.Timeout != 2*time.Second {
		t.Fatalf("WakeTargets = %+v", cfg.WakeTargets)
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", cfg.HTTP.ReadHeaderTimeout)
	}
}

func TestLoadConfigAllowsWakeTargetsOmitted(t *testing.T) {
	t.Setenv("DEN_CHANNELS_DATABASE_URL", "postgres://channels.example/denservices")
	t.Setenv("DEN_CONVERSATION_SERVICE_TOKEN", "conversation-token")

	cfg, err := LoadConfigFromPath(writeTestConfigWithoutWakeTargets(t))
	if err != nil {
		t.Fatalf("LoadConfigFromPath() error = %v", err)
	}
	if cfg.WakeTargets.Enabled {
		t.Fatal("WakeTargets.Enabled = true, want false")
	}
}

func TestLoadConfigRequiresServiceToken(t *testing.T) {
	t.Setenv("DEN_CHANNELS_DATABASE_URL", "postgres://channels.example/denservices")
	t.Setenv("DEN_CONVERSATION_SERVICE_TOKEN", "")

	_, err := LoadConfigFromPath(writeTestConfig(t))
	if !errors.Is(err, ErrMissingServiceToken) {
		t.Fatalf("LoadConfigFromPath() error = %v, want %v", err, ErrMissingServiceToken)
	}
}

func writeTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`
bind_addr: "127.0.0.1:8084"
database_url: "${DEN_CHANNELS_DATABASE_URL}"

query:
  default_limit: 100
  max_limit: 500

wake_targets:
  runtime_base_url: "http://127.0.0.1:8081"
  runtime_service_token_env: "DEN_CONVERSATION_RUNTIME_SERVICE_TOKEN"
  timeout: "2s"

http:
  read_header_timeout: "5s"
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func writeTestConfigWithoutWakeTargets(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`
bind_addr: "127.0.0.1:8084"
database_url: "${DEN_CHANNELS_DATABASE_URL}"

query:
  default_limit: 100
  max_limit: 500

http:
  read_header_timeout: "5s"
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
