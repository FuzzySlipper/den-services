package registry

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
)

func TestDefaultToolSchemaSnapshot(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	actual, err := json.MarshalIndent(registry.Tools(), "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	actual = append(actual, '\n')

	const wantHash = "5f9ca6d88377ec88d8e85358ca3640b6658c1f70bb001c32769aba3aab740bb2"
	actualHash := fmt.Sprintf("%x", sha256.Sum256(actual))
	if actualHash != wantHash {
		t.Fatalf("default tool schema snapshot hash = %s, want %s\n%s", actualHash, wantHash, string(actual))
	}
}
