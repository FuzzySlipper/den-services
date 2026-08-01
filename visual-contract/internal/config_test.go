package visualcontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigUsesPublicArtifactBasePath(t *testing.T) {
	t.Setenv("DEN_VISUAL_CONTRACT_SERVICE_TOKEN", "test-token")
	path := writeVisualContractConfig(t, "/api/v1/visual-contracts")

	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("LoadConfigFromPath() error = %v", err)
	}
	if cfg.Artifacts.PublicBasePath != "/api/v1/visual-contracts" {
		t.Fatalf("PublicBasePath = %q", cfg.Artifacts.PublicBasePath)
	}
}

func TestLoadConfigRejectsPrivateArtifactURL(t *testing.T) {
	t.Setenv("DEN_VISUAL_CONTRACT_SERVICE_TOKEN", "test-token")
	path := writeVisualContractConfig(t, "http://127.0.0.1:8086/visual-contracts")

	_, err := LoadConfigFromPath(path)
	if err == nil {
		t.Fatal("LoadConfigFromPath() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "artifacts.public_base_path") {
		t.Fatalf("LoadConfigFromPath() error = %v, want public_base_path context", err)
	}
}

func writeVisualContractConfig(t *testing.T, publicBasePath string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := `bind_addr: "127.0.0.1:8086"
artifacts:
  public_base_path: "` + publicBasePath + `"
  path: "` + t.TempDir() + `"
http:
  read_header_timeout: "5s"
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return configPath
}
