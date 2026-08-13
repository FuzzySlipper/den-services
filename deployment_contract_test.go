package denservices

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type serviceRegistry struct {
	Services []deployableService `yaml:"services"`
}

type deployableService struct {
	Name          string `yaml:"name"`
	Module        string `yaml:"module"`
	BinaryName    string `yaml:"binary_name"`
	BinaryPath    string `yaml:"binary_path"`
	ConfigExample string `yaml:"config_example"`
	EnvExample    string `yaml:"env_example"`
	HealthURL     string `yaml:"health_url"`
	VersionURL    string `yaml:"version_url"`
	SystemdUnit   string `yaml:"systemd_unit"`
}

func TestDeployableServicesContract(t *testing.T) {
	registry := loadServiceRegistry(t)
	if len(registry.Services) == 0 {
		t.Fatal("deployment/services.yaml must register at least one deployable service")
	}
	seen := make(map[string]bool, len(registry.Services))
	for _, service := range registry.Services {
		service := service
		t.Run(service.Name, func(t *testing.T) {
			validateServiceMetadata(t, service, seen)
			assertVersionCommand(t, service)
		})
	}
}

func TestDeploymentSafetyRegression(t *testing.T) {
	command := exec.Command("bash", "scripts/tests/deploy-safety-test.sh")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("deploy safety regression failed: %v\n%s", err, output)
	}

	deployScript, err := os.ReadFile("scripts/den-services-deploy.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(deployScript)
	for _, required := range []string{
		"DEN_GATEWAY_KNOWLEDGE_UPSTREAM_TOKEN",
		"DEN_GATEWAY_BOARD_UPSTREAM_TOKEN",
		"DEN_HANDOFF_SERVICE_TOKEN",
		"DEN_BOARD_SERVICE_TOKEN",
		"den-go@handoff.service must be active",
		"den-go@board.service must be active",
		"ensure_gateway_board_routes",
		"ensure_mcp_board_backend",
		"rollback/routes.yaml",
		"systemctl reset-failed",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("deploy script missing safety contract %q", required)
		}
	}
	safetyHelper, err := os.ReadFile("scripts/lib/deploy-safety.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(safetyHelper), `sudo -n grep -Eq`) {
		t.Fatal("env preflight must support root-readable secret files without printing values")
	}

	fleetScript, err := os.ReadFile("scripts/update-den-fleet.sh")
	if err != nil {
		t.Fatal(err)
	}
	fleetText := string(fleetScript)
	for _, required := range []string{
		"DEN_GATEWAY_KNOWLEDGE_UPSTREAM_TOKEN",
		"den-go@handoff.service is not active",
		"mcp_config",
		"review librarian handoff",
	} {
		if !strings.Contains(fleetText, required) {
			t.Fatalf("fleet script missing preflight contract %q", required)
		}
	}
}

func TestBoardUpgradeConfigMigration(t *testing.T) {
	helper := "scripts/lib/ensure-board-config.py"
	for name, source := range map[string]string{
		"indentless": "server:\n  listen_addr: loopback\nbackends:\n- name: projects\n  base_url: http://127.0.0.1:8091\n",
		"indented":   "server:\n  listen_addr: loopback\nbackends:\n  - name: projects\n    base_url: http://127.0.0.1:8091\n",
	} {
		t.Run("mcp-"+name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if output, err := exec.Command("python3", helper, "mcp-backend", path).CombinedOutput(); err != nil {
					t.Fatalf("mcp migration failed: %v\n%s", err, output)
				}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var parsed map[string]any
			if err := yaml.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("migrated MCP config is invalid YAML: %v\n%s", err, data)
			}
			if strings.Count(string(data), `name: "board"`) != 1 {
				t.Fatalf("Board backend is not idempotent:\n%s", data)
			}
		})
	}

	routesPath := filepath.Join(t.TempDir(), "routes.yaml")
	routes := "routes:\n  - name: projects-routes\n    path_pattern: \"/v1/projects\"\n    methods: [\"GET\"]\n"
	if err := os.WriteFile(routesPath, []byte(routes), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if output, err := exec.Command("python3", helper, "gateway-routes", routesPath).CombinedOutput(); err != nil {
			t.Fatalf("gateway migration failed: %v\n%s", err, output)
		}
	}
	data, err := os.ReadFile(routesPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("migrated Gateway config is invalid YAML: %v\n%s", err, data)
	}
	text := string(data)
	if strings.Count(text, "board-project-routes") != 1 || strings.Count(text, "board-item-routes") != 1 {
		t.Fatalf("Board routes are not idempotent:\n%s", data)
	}
	if strings.Index(text, "board-project-routes") > strings.Index(text, "projects-routes") {
		t.Fatalf("Board project route must precede broad projects route:\n%s", data)
	}
}

func loadServiceRegistry(t *testing.T) serviceRegistry {
	t.Helper()
	data, err := os.ReadFile("deployment/services.yaml")
	if err != nil {
		t.Fatalf("ReadFile(deployment/services.yaml) error = %v", err)
	}
	var registry serviceRegistry
	if err := yaml.Unmarshal(data, &registry); err != nil {
		t.Fatalf("Unmarshal(deployment/services.yaml) error = %v", err)
	}
	return registry
}

func validateServiceMetadata(t *testing.T, service deployableService, seen map[string]bool) {
	t.Helper()
	required := map[string]string{
		"name":           service.Name,
		"module":         service.Module,
		"binary_name":    service.BinaryName,
		"binary_path":    service.BinaryPath,
		"config_example": service.ConfigExample,
		"env_example":    service.EnvExample,
		"health_url":     service.HealthURL,
		"version_url":    service.VersionURL,
		"systemd_unit":   service.SystemdUnit,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s is required", field)
		}
	}
	if seen[service.Name] {
		t.Fatalf("duplicate service name %s", service.Name)
	}
	seen[service.Name] = true
	assertFileExists(t, service.ConfigExample)
	assertFileExists(t, service.EnvExample)
	if !strings.HasPrefix(service.BinaryPath, "./") {
		t.Fatalf("binary_path %q must be repo-relative and start with ./", service.BinaryPath)
	}
	if !strings.HasPrefix(service.HealthURL, "http://127.0.0.1:") {
		t.Fatalf("health_url %q must be loopback", service.HealthURL)
	}
	if !strings.HasPrefix(service.VersionURL, "http://127.0.0.1:") {
		t.Fatalf("version_url %q must be loopback", service.VersionURL)
	}
	if service.SystemdUnit != "den-go@"+service.Name+".service" {
		t.Fatalf("systemd_unit = %q, want den-go@%s.service", service.SystemdUnit, service.Name)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("%s is a directory, want file", path)
	}
}

func assertVersionCommand(t *testing.T, service deployableService) {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), service.BinaryName)
	build := exec.Command("go", "build", "-trimpath", "-o", binaryPath, service.BinaryPath)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build %s error = %v\n%s", service.BinaryPath, err, string(output))
	}
	version := exec.Command(binaryPath, "--version")
	output, err := version.CombinedOutput()
	if err != nil {
		t.Fatalf("%s --version error = %v\n%s", service.BinaryName, err, string(output))
	}
	text := strings.TrimSpace(string(output))
	if !strings.Contains(text, service.Name) {
		t.Fatalf("--version output %q must include service name %q", text, service.Name)
	}
	if !strings.Contains(text, "dev") || !strings.Contains(text, "unknown") {
		t.Fatalf("--version output %q must include default dev build metadata", text)
	}
}
