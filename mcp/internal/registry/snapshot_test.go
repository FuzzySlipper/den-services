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

	const wantHash = "c329157ac759a8bcbccbda10039e076ef63191a4a0480070f4d300b98096acad"
	actualHash := fmt.Sprintf("%x", sha256.Sum256(actual))
	if actualHash != wantHash {
		t.Fatalf("default tool schema snapshot hash = %s, want %s\n%s", actualHash, wantHash, string(actual))
	}
}
