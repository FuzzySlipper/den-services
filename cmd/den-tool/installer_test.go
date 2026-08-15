package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerSyntaxAndIdempotentCheck(t *testing.T) {
	root := repositoryRootForTest(t)
	installer := filepath.Join(root, "scripts", "install-den-tool.sh")
	if output, err := exec.Command("bash", "-n", installer).CombinedOutput(); err != nil {
		t.Fatalf("bash -n installer: %v\n%s", err, output)
	}

	home := t.TempDir()
	firstOutput, err := runInstaller(t, root, home)
	if err != nil {
		t.Fatalf("first installer run: %v\n%s", err, firstOutput)
	}
	destination := filepath.Join(home, ".local", "bin", "den-tool")
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("installed permissions = %o, want 755", info.Mode().Perm())
	}
	if output, err := exec.Command(destination, "--version").CombinedOutput(); err != nil {
		t.Fatalf("installed --version: %v\n%s", err, output)
	} else if !strings.HasPrefix(string(output), "den-tool version ") {
		t.Fatalf("installed version = %q, want den-tool marker", output)
	}

	if output, err := runInstaller(t, root, home, "--check"); err != nil {
		t.Fatalf("installer --check: %v\n%s", err, output)
	}
	if output, err := runInstaller(t, root, home); err != nil {
		t.Fatalf("second installer run: %v\n%s", err, output)
	}
}

func TestInstallerRefusesUnrelatedBinary(t *testing.T) {
	root := repositoryRootForTest(t)
	home := t.TempDir()
	destination := filepath.Join(home, ".local", "bin", "den-tool")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("create bin directory: %v", err)
	}
	if err := os.WriteFile(destination, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write unrelated binary: %v", err)
	}
	output, err := runInstaller(t, root, home)
	if err == nil {
		t.Fatalf("installer succeeded over unrelated binary; output=%s", output)
	}
	if !strings.Contains(string(output), "refusing to overwrite unrelated binary") {
		t.Fatalf("installer output = %q, want ownership refusal", output)
	}
}

func TestInstallerDoesNotExecuteVersionMimickingUnrelatedBinary(t *testing.T) {
	root := repositoryRootForTest(t)
	home := t.TempDir()
	destination := filepath.Join(home, ".local", "bin", "den-tool")
	proof := filepath.Join(home, "unrelated-executed")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ntouch \"" + proof + "\"\nprintf 'den-tool version 1.0.0\\n'\n"
	if err := os.WriteFile(destination, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := runInstaller(t, root, home)
	if err == nil {
		t.Fatalf("installer accepted version-mimicking binary: %s", output)
	}
	if _, err := os.Stat(proof); !os.IsNotExist(err) {
		t.Fatalf("installer executed unrelated binary; stat error = %v", err)
	}
}

func TestInstallerRefusesBinaryChangedBehindOwnedMarker(t *testing.T) {
	root := repositoryRootForTest(t)
	home := t.TempDir()
	if output, err := runInstaller(t, root, home); err != nil {
		t.Fatalf("initial installer run: %v\n%s", err, output)
	}
	destination := filepath.Join(home, ".local", "bin", "den-tool")
	if err := os.WriteFile(destination, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("replace installed binary: %v", err)
	}
	output, err := runInstaller(t, root, home)
	if err == nil {
		t.Fatalf("installer succeeded over replaced binary; output=%s", output)
	}
	if !strings.Contains(string(output), "refusing to overwrite unrelated binary") {
		t.Fatalf("installer output = %q, want ownership refusal", output)
	}
}

func TestInstallerCheckDetectsSourceDriftUntilReinstall(t *testing.T) {
	sourceRoot := repositoryRootForTest(t)
	root := t.TempDir()
	for _, relative := range []string{"go.mod", "go.sum", "scripts/install-den-tool.sh"} {
		copyTestFile(t, sourceRoot, root, relative)
	}
	entries, err := os.ReadDir(filepath.Join(sourceRoot, "cmd", "den-tool"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), ".json")) {
			copyTestFile(t, sourceRoot, root, filepath.Join("cmd", "den-tool", entry.Name()))
		}
	}

	home := t.TempDir()
	if output, err := runInstaller(t, root, home); err != nil {
		t.Fatalf("initial installer run: %v\n%s", err, output)
	}
	catalogPath := filepath.Join(root, "cmd", "den-tool", "catalog.json")
	catalog, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, append(catalog, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := runInstaller(t, root, home, "--check"); err == nil || !strings.Contains(string(output), "source identity drifted") {
		t.Fatalf("drift check output = %q, error = %v", output, err)
	}
	if output, err := runInstaller(t, root, home); err != nil {
		t.Fatalf("reinstall after drift: %v\n%s", err, output)
	}
	if output, err := runInstaller(t, root, home, "--check"); err != nil {
		t.Fatalf("check after reinstall: %v\n%s", err, output)
	}
}

func copyTestFile(t *testing.T, sourceRoot, destinationRoot, relative string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(sourceRoot, relative))
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(destinationRoot, relative)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

func runInstaller(t *testing.T, root, home string, args ...string) ([]byte, error) {
	t.Helper()
	installer := filepath.Join(root, "scripts", "install-den-tool.sh")
	commandArgs := append([]string{installer}, args...)
	command := exec.Command("bash", commandArgs...)
	command.Dir = root
	command.Env = environmentWithHome(os.Environ(), home)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return output.Bytes(), err
}

func repositoryRootForTest(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get test working directory: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(workingDirectory, "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}

func environmentWithHome(environment []string, home string) []string {
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if strings.HasPrefix(entry, "HOME=") {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "HOME="+home)
}
