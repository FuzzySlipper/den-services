package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPointerFirstFixturesAreBoundedAndSelfIdentifying(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "testdata", "pointer-first"))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("fixture count = %d, want 4", len(entries))
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			t.Fatalf("unexpected fixture entry %q", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join("..", "testdata", "pointer-first", entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", entry.Name(), err)
		}
		var fixture struct {
			Name          string `json:"name"`
			Schema        string `json:"schema"`
			SchemaVersion int    `json:"schema_version"`
			WorkflowKey   struct {
				ProjectID     string `json:"project_id"`
				TaskID        int64  `json:"task_id"`
				ReviewRoundID int64  `json:"review_round_id"`
				HeadCommit    string `json:"head_commit"`
				CorrelationID string `json:"correlation_id"`
			} `json:"workflow_key"`
			Revision       int    `json:"revision"`
			MaterialDigest string `json:"material_digest"`
			Expected       struct {
				SerializedBytesMax int    `json:"serialized_bytes_max"`
				MaterialEventKey   string `json:"material_event_key"`
			} `json:"expected"`
		}
		if err := json.Unmarshal(data, &fixture); err != nil {
			t.Fatalf("Unmarshal(%q) error = %v", entry.Name(), err)
		}
		if fixture.Name == "" || fixture.Schema == "" || !strings.HasSuffix(fixture.Schema, ".v1") || fixture.SchemaVersion != 1 || fixture.Revision <= 0 || fixture.MaterialDigest == "" {
			t.Fatalf("fixture %q has incomplete identity: %#v", entry.Name(), fixture)
		}
		if fixture.WorkflowKey.ProjectID == "" || fixture.WorkflowKey.TaskID <= 0 || fixture.WorkflowKey.ReviewRoundID <= 0 || fixture.WorkflowKey.HeadCommit == "" || fixture.WorkflowKey.CorrelationID == "" {
			t.Fatalf("fixture %q has incomplete workflow key: %#v", entry.Name(), fixture.WorkflowKey)
		}
		if fixture.Expected.SerializedBytesMax <= 0 || fixture.Expected.MaterialEventKey == "" || len(data) > fixture.Expected.SerializedBytesMax {
			t.Fatalf("fixture %q exceeds or omits its declared budget: bytes=%d expected=%#v", entry.Name(), len(data), fixture.Expected)
		}
	}
}
