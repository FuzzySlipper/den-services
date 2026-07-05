package backend

import (
	"encoding/json"
	"testing"
)

func TestGuidanceCompatibleResponseBodyMapsPacketToLegacyShape(t *testing.T) {
	body := []byte(`{
		"project_id":"den-services",
		"resolved_at":"2026-07-01T12:00:00Z",
		"content_markdown":"# Den Agent Guidance\n\nUse the real service.",
		"content_sha256":"abc123",
		"content_bytes":42,
		"truncated":false,
		"incomplete":false,
		"sources":[{
			"entry_id":7,
			"source_scope":"_global",
			"document_project_id":"_global",
			"document_slug":"agent-policy",
			"document_title":"Agent Policy",
			"document_type":"convention",
			"document_updated_at":"2026-07-01T11:00:00Z",
			"visibility":"normal",
			"tags":["agents","policy"],
			"importance":"required",
			"audience":["all"],
			"sort_order":10,
			"notes":"Read first.",
			"content_bytes":123
		}]
	}`)

	compatible, err := guidanceCompatibleResponseBody("get_agent_guidance", body)
	if err != nil {
		t.Fatalf("guidanceCompatibleResponseBody() error = %v", err)
	}
	var legacy map[string]any
	if err := json.Unmarshal(compatible, &legacy); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", string(compatible), err)
	}
	if legacy["content"] != "# Den Agent Guidance\n\nUse the real service." {
		t.Fatalf("content = %v", legacy["content"])
	}
	if _, ok := legacy["content_markdown"]; !ok {
		t.Fatal("content_markdown additive field missing")
	}
	sources := legacy["sources"].([]any)
	source := sources[0].(map[string]any)
	for _, key := range []string{"scope_project_id", "slug", "title", "doc_type", "tags", "updated_at"} {
		if _, ok := source[key]; !ok {
			t.Fatalf("legacy source missing %s: %#v", key, source)
		}
	}
	if source["source_scope"] != nil {
		t.Fatalf("source_scope leaked into legacy source: %#v", source)
	}
}

func TestGuidanceCompatibleResponseBodyUnwrapsEntryList(t *testing.T) {
	body := []byte(`{"entries":[{"id":1,"project_id":"_global","document_slug":"agent-policy"}],"count":1}`)

	compatible, err := guidanceCompatibleResponseBody("list_agent_guidance_entries", body)
	if err != nil {
		t.Fatalf("guidanceCompatibleResponseBody() error = %v", err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(compatible, &entries); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", string(compatible), err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(entries))
	}
	if entries[0]["id"] != float64(1) {
		t.Fatalf("entry id = %v, want 1", entries[0]["id"])
	}
}

func TestGuidanceRESTToolResultUsesCompatibleShape(t *testing.T) {
	body := []byte(`{"entries":[{"id":1,"project_id":"_global","document_slug":"agent-policy"}],"count":1}`)
	compatible, err := guidanceCompatibleResponseBody("list_agent_guidance_entries", body)
	if err != nil {
		t.Fatalf("guidanceCompatibleResponseBody() error = %v", err)
	}
	resultBody, err := buildRESTToolResult(compatible)
	if err != nil {
		t.Fatalf("buildRESTToolResult() error = %v", err)
	}
	var result mcpToolResult
	if err := json.Unmarshal(resultBody, &result); err != nil {
		t.Fatalf("Unmarshal(result) error = %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != `[{"id":1,"project_id":"_global","document_slug":"agent-policy"}]` {
		t.Fatalf("content text = %#v", result.Content)
	}
	var structured struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(result.StructuredContent, &structured); err != nil {
		t.Fatalf("structured content is not object-wrapped legacy array: %v", err)
	}
	if len(structured.Items) != 1 || structured.Items[0]["id"] != float64(1) {
		t.Fatalf("structured items = %#v", structured.Items)
	}
}
