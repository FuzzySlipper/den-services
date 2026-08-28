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

	const wantHash = "94ba0252c0f5c70d1535192729b5f43f08817cd623f0239b3c1ddeb2c11d1a3c"
	actualHash := fmt.Sprintf("%x", sha256.Sum256(actual))
	if actualHash != wantHash {
		t.Fatalf("default tool schema snapshot hash = %s, want %s\n%s", actualHash, wantHash, string(actual))
	}
}
