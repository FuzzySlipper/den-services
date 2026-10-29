package ops

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExampleConfigLoads(t *testing.T) {
	cfg, err := LoadConfigFromPath(filepath.Join("..", "config", "config.example.yaml"))
	if err != nil {
		t.Fatalf("LoadConfigFromPath: %v", err)
	}
	if cfg.DatabasePath != "/data/services/ops/data/ops.sqlite3" || cfg.Defaults.HeartbeatStaleAfter != 15*time.Minute {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestConfigRejectsMissingDatabasePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "bind_addr: \"127.0.0.1:8102\"\nhttp:\n  read_header_timeout: \"5s\"\ndefaults:\n  heartbeat_stale_after: \"15m\"\n  run_list_limit: 50\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfigFromPath(path); err == nil {
		t.Fatal("config without database_path loaded")
	}
}
