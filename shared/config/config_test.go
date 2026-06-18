package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadReadsEnvironmentAndEnvFile(t *testing.T) {
	t.Setenv("DEN_TEST_VALUE", "from-env")
	path := writeTempEnvFile(t, `DEN_TEST_VALUE=from-file
DEN_OTHER_VALUE="quoted value"
`)

	values, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := values.String("DEN_TEST_VALUE", "default"); got != "from-file" {
		t.Fatalf("String() = %q, want from-file", got)
	}
	if got := values.String("DEN_OTHER_VALUE", "default"); got != "quoted value" {
		t.Fatalf("String() = %q, want quoted value", got)
	}
}

func TestTypedDefaults(t *testing.T) {
	values := FromMap(map[string]string{
		"BOOL":     "true",
		"INT":      "42",
		"DURATION": "5s",
	})

	gotBool, err := values.Bool("BOOL", false)
	if err != nil {
		t.Fatalf("Bool() error = %v", err)
	}
	if !gotBool {
		t.Fatal("Bool() = false, want true")
	}

	gotInt, err := values.Int("INT", 0)
	if err != nil {
		t.Fatalf("Int() error = %v", err)
	}
	if gotInt != 42 {
		t.Fatalf("Int() = %d, want 42", gotInt)
	}

	gotDuration, err := values.Duration("DURATION", time.Second)
	if err != nil {
		t.Fatalf("Duration() error = %v", err)
	}
	if gotDuration != 5*time.Second {
		t.Fatalf("Duration() = %s, want 5s", gotDuration)
	}
}

func TestRequiredStringReportsMissingValue(t *testing.T) {
	values := FromMap(map[string]string{})
	_, err := values.RequiredString("MISSING")
	if !errors.Is(err, ErrMissingValue) {
		t.Fatalf("RequiredString() error = %v, want %v", err, ErrMissingValue)
	}
}

func TestExpandRequiresValues(t *testing.T) {
	values := FromMap(map[string]string{"TOKEN": "secret"})

	got, err := values.Expand("bearer:${TOKEN}")
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if got != "bearer:secret" {
		t.Fatalf("Expand() = %q, want bearer:secret", got)
	}

	_, err = values.Expand("${MISSING}")
	if !errors.Is(err, ErrMissingValue) {
		t.Fatalf("Expand() error = %v, want %v", err, ErrMissingValue)
	}
}

func TestLoadRejectsInvalidEnvFileLine(t *testing.T) {
	path := writeTempEnvFile(t, "not-a-pair")
	_, err := Load(path)
	if !errors.Is(err, ErrInvalidEnvFileLine) {
		t.Fatalf("Load() error = %v, want %v", err, ErrInvalidEnvFileLine)
	}
}

func writeTempEnvFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
