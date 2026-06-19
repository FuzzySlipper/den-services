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
