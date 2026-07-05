package serve

import (
	"os"
	"path/filepath"
	"testing"

	devserver "den-services/devserver-broker"
)

func TestLoadConfigDefaultsToLanFacingBindAndLoopbackProbe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
state_dir: "~/.cache/den-serve/state"
session_root: "~/.cache/den-serve/sessions"
public_host: "auto"
port_range:
  start: 37300
  end: 37450
timeouts:
  lock_timeout: "10s"
  startup_timeout: "45s"
  health_timeout: "2s"
  health_interval: "250ms"
  shutdown_timeout: "5s"
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("LoadConfigFromPath() error = %v", err)
	}
	if cfg.BindHost != devserver.DefaultBindHost {
		t.Fatalf("BindHost = %q, want %q", cfg.BindHost, devserver.DefaultBindHost)
	}
	if cfg.ProbeHost != devserver.DefaultProbeHost {
		t.Fatalf("ProbeHost = %q, want %q", cfg.ProbeHost, devserver.DefaultProbeHost)
	}
}

func TestDefaultConfigSupportsOneCommandWorkflow(t *testing.T) {
	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() error = %v", err)
	}
	if cfg.BindHost != devserver.DefaultBindHost {
		t.Fatalf("BindHost = %q, want %q", cfg.BindHost, devserver.DefaultBindHost)
	}
	if cfg.ProbeHost != devserver.DefaultProbeHost {
		t.Fatalf("ProbeHost = %q, want %q", cfg.ProbeHost, devserver.DefaultProbeHost)
	}
	if cfg.PortRange.Start != 37300 || cfg.PortRange.End != 37450 {
		t.Fatalf("PortRange = %#v, want 37300-37450", cfg.PortRange)
	}
}
