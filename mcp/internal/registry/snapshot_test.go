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

	const wantHash = "ba9a372ff6a1b529dd75735f0d4b291e184962d7b6ced09280a34e4868a0a5c3"
	actualHash := fmt.Sprintf("%x", sha256.Sum256(actual))
	if actualHash != wantHash {
		t.Fatalf("default tool schema snapshot hash = %s, want %s\n%s", actualHash, wantHash, string(actual))
	}
}
