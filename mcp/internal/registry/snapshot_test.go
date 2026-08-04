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

	const wantHash = "93b323d322ea3e6f39d78d5d86295d1a041f25cbaee3da859159dce27c3e6fbb"
	actualHash := fmt.Sprintf("%x", sha256.Sum256(actual))
	if actualHash != wantHash {
		t.Fatalf("default tool schema snapshot hash = %s, want %s\n%s", actualHash, wantHash, string(actual))
	}
}
