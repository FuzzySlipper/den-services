package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"den-services/mcp/internal/config"
)

func TestAssignmentManifestOrdersDeduplicatesAndKeepsInstructionsSeparate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/7426":
			_, _ = w.Write([]byte(`{"task":{"id":7426,"project_id":"den-services","title":"Context manifests","status":"in_progress"}}`))
		case "/v1/guidance/context-resolve":
			var request struct {
				InlineBudget int `json:"inline_budget"`
				Scopes       []struct {
					Kind string `json:"kind"`
					Ref  string `json:"ref"`
				} `json:"scopes"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.InlineBudget != 256 || len(request.Scopes) != 3 {
				t.Fatalf("guidance request = %#v", request)
			}
			_, _ = w.Write([]byte(`{"selections":[
				{"target_kind":"knowledge","target_ref":"mcp-routing","scope_kind":"task","priority":3,"sort_order":2,"read_policy":"must_read","read_when":"MCP route work","selection_source":"knowledge_binding","target":{"slug":"mcp-routing","title":"MCP routing","summary":"Route adapter boundary.","kind":"reference","tags":["mcp","routing"],"revision":4,"revision_known":true}},
				{"target_kind":"knowledge","target_ref":"database-schema","scope_kind":"project","priority":8,"sort_order":1,"read_policy":"on_demand","read_when":"SQL migrations only","selection_source":"knowledge_binding","target":{"slug":"database-schema","title":"Database schema","summary":"Migration guidance.","kind":"reference","tags":["postgres"],"revision":2,"revision_known":true}}
			]}`))
		case "/v1/knowledge/entries/explicit-guide/card":
			_, _ = w.Write([]byte(`{"slug":"explicit-guide","title":"Explicit guide","summary":"Selected reference.","kind":"reference","tags":["mcp"],"revision":7}`))
		case "/v1/projects/den-services/librarian/query":
			_, _ = w.Write([]byte(`{"relevant_items":[{"type":"knowledge","source_id":"mcp-routing","title":"Duplicate route card"},{"type":"document","project_id":"den-services","source_id":"map","title":"Nearby map","summary":"Topology map."}]}`))
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	}))
	defer server.Close()
	locator := newAssignmentManifestLocator(t, server)
	result, failure, err := locator.Call(context.Background(), ToolCall{ToolName: "compose_assignment_manifest", Operation: "compose_assignment_manifest", Arguments: json.RawMessage(`{"task_id":7426,"assignment":"Implement MCP route adapter","background":"Do not preload bodies.","agent_profile":"coder","capabilities":["mcp"],"explicit_knowledge_refs":["explicit-guide"],"inherited_refs":[{"reference":"knowledge:parent-only","title":"Parent-only contract","summary":"Contract selected by the parent assignment.","kind":"reference","revision":9,"read_when":"When preserving the parent boundary."}],"limits":{"max_handles":5,"inline_budget":256,"librarian_items":2}}`)})
	if err != nil || failure != nil {
		t.Fatalf("Call() = %v, %#v", err, failure)
	}
	manifest := decodeAssignmentManifest(t, result)
	if len(manifest.References) != 4 {
		t.Fatalf("references = %#v", manifest.References)
	}
	if manifest.References[0].Reference != "knowledge:mcp-routing" || manifest.References[0].SelectionSource != "knowledge_binding" {
		t.Fatalf("must-read/dedup entry = %#v", manifest.References[0])
	}
	if manifest.References[1].Reference != "knowledge:explicit-guide" || manifest.References[1].SelectionSource != "explicit" {
		t.Fatalf("explicit entry = %#v", manifest.References[1])
	}
	if manifest.References[2].Reference != "knowledge:parent-only" || manifest.References[2].SelectionSource != "inherited_manifest" || manifest.References[2].Revision == nil || *manifest.References[2].Revision != 9 || manifest.References[2].ReadCostBytes == 0 {
		t.Fatalf("inherited entry = %#v", manifest.References[2])
	}
	if manifest.References[3].Reference != "document:den-services/map" {
		t.Fatalf("librarian entry = %#v", manifest.References[3])
	}
	if strings.Contains(manifest.Markdown, "Route adapter boundary.") || !strings.Contains(manifest.Markdown, "### Instructions") || !strings.Contains(manifest.Markdown, "### Optional background") {
		t.Fatalf("markdown did not preserve compact separation: %s", manifest.Markdown)
	}
	if manifest.References[0].Revision == nil || *manifest.References[0].Revision != 4 {
		t.Fatalf("revision handle = %#v", manifest.References[0])
	}
}

func TestAssignmentManifestIsNarrowAndSiblingsDiffer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/7426":
			_, _ = w.Write([]byte(`{"task":{"id":7426,"project_id":"den-services","title":"Parent","status":"in_progress"}}`))
		case "/v1/guidance/context-resolve":
			_, _ = w.Write([]byte(`{"selections":[
				{"target_kind":"knowledge","target_ref":"routing","read_policy":"on_demand","read_when":"routing adapter","selection_source":"knowledge_binding","target":{"slug":"routing","title":"Routing adapter","summary":"Route contract.","kind":"reference","tags":["routing"]}},
				{"target_kind":"knowledge","target_ref":"storage","read_policy":"on_demand","read_when":"postgres store","selection_source":"knowledge_binding","target":{"slug":"storage","title":"Postgres store","summary":"Store contract.","kind":"reference","tags":["postgres"]}}
			]}`))
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	}))
	defer server.Close()
	locator := newAssignmentManifestLocatorWithoutOptional(t, server)
	first, failure, err := locator.Call(context.Background(), ToolCall{Operation: "compose_assignment_manifest", Arguments: json.RawMessage(`{"task_id":7426,"assignment":"Update routing adapter","limits":{"librarian_items":0}}`)})
	if err != nil || failure != nil {
		t.Fatalf("first call = %v, %#v", err, failure)
	}
	second, failure, err := locator.Call(context.Background(), ToolCall{Operation: "compose_assignment_manifest", Arguments: json.RawMessage(`{"task_id":7426,"assignment":"Update postgres store","limits":{"librarian_items":0}}`)})
	if err != nil || failure != nil {
		t.Fatalf("second call = %v, %#v", err, failure)
	}
	firstManifest, secondManifest := decodeAssignmentManifest(t, first), decodeAssignmentManifest(t, second)
	if len(firstManifest.References) != 1 || len(secondManifest.References) != 1 || firstManifest.References[0].Reference == secondManifest.References[0].Reference {
		t.Fatalf("siblings were not narrowed: %#v %#v", firstManifest.References, secondManifest.References)
	}
}

func TestAssignmentManifestHonorsBudgetAndReportsUnavailableOptionalSources(t *testing.T) {
	var librarianCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/7426":
			_, _ = w.Write([]byte(`{"task":{"id":7426,"project_id":"den-services","title":"Task","status":"planned"}}`))
		case "/v1/guidance/context-resolve":
			http.Error(w, "guidance unavailable", http.StatusBadGateway)
		case "/v1/projects/den-services/librarian/query":
			librarianCalls.Add(1)
			http.Error(w, "librarian unavailable", http.StatusBadGateway)
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	}))
	defer server.Close()
	locator := newAssignmentManifestLocator(t, server)
	result, failure, err := locator.Call(context.Background(), ToolCall{Operation: "compose_assignment_manifest", Arguments: json.RawMessage(`{"task_id":7426,"assignment":"Narrow work","inherited_refs":[{"reference":"knowledge:one","title":"One","summary":"First inherited card.","kind":"reference","revision":1,"read_when":"When one applies."},{"reference":"knowledge:two","title":"Two","summary":"Second inherited card.","kind":"reference","update_marker":"2026-08-28T00:00:00Z","read_when":"When two applies."}],"limits":{"max_handles":1,"inline_budget":0,"librarian_items":0}}`)})
	if err != nil || failure != nil {
		t.Fatalf("Call() = %v, %#v", err, failure)
	}
	manifest := decodeAssignmentManifest(t, result)
	if len(manifest.References) != 1 || !manifest.Truncated || librarianCalls.Load() != 0 {
		t.Fatalf("budget result = %#v, librarian calls = %d", manifest, librarianCalls.Load())
	}
	if !strings.Contains(string(result.Value), `"source":"guidance","state":"unavailable"`) {
		t.Fatalf("missing degraded source: %s", result.Value)
	}
}

func TestAssignmentManifestExcludesIncompleteInheritedCards(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/7426":
			_, _ = w.Write([]byte(`{"task":{"id":7426,"project_id":"den-services","title":"Task","status":"planned"}}`))
		case "/v1/guidance/context-resolve":
			_, _ = w.Write([]byte(`{"selections":[]}`))
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	}))
	defer server.Close()
	locator := newAssignmentManifestLocatorWithoutOptional(t, server)
	result, failure, err := locator.Call(context.Background(), ToolCall{Operation: "compose_assignment_manifest", Arguments: json.RawMessage(`{"task_id":7426,"assignment":"Narrow work","inherited_refs":[{"reference":"knowledge:missing-summary","revision":1,"read_when":"When relevant."},{"reference":"knowledge:missing-freshness","summary":"No revision.","read_when":"When relevant."},{"reference":"knowledge:missing-trigger","summary":"No trigger.","revision":1}],"limits":{"librarian_items":0}}`)})
	if err != nil || failure != nil {
		t.Fatalf("Call() = %v, %#v", err, failure)
	}
	manifest := decodeAssignmentManifest(t, result)
	if len(manifest.References) != 0 {
		t.Fatalf("incomplete inherited cards were emitted: %#v", manifest.References)
	}
}

func TestAssignmentManifestReportsUnavailableLibrarian(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/7426":
			_, _ = w.Write([]byte(`{"task":{"id":7426,"project_id":"den-services","title":"Task","status":"planned"}}`))
		case "/v1/guidance/context-resolve":
			_, _ = w.Write([]byte(`{"selections":[]}`))
		case "/v1/projects/den-services/librarian/query":
			http.Error(w, "librarian unavailable", http.StatusBadGateway)
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	}))
	defer server.Close()
	locator := newAssignmentManifestLocator(t, server)
	result, failure, err := locator.Call(context.Background(), ToolCall{Operation: "compose_assignment_manifest", Arguments: json.RawMessage(`{"task_id":7426,"assignment":"Narrow work","limits":{"librarian_items":1}}`)})
	if err != nil || failure != nil {
		t.Fatalf("Call() = %v, %#v", err, failure)
	}
	if !strings.Contains(string(result.Value), `"source":"librarian","state":"unavailable"`) {
		t.Fatalf("missing degraded librarian source: %s", result.Value)
	}
}

func decodeAssignmentManifest(t *testing.T, result Result) assignmentManifestResponse {
	t.Helper()
	var toolResult mcpToolResult
	if err := json.Unmarshal(result.Value, &toolResult); err != nil {
		t.Fatal(err)
	}
	var manifest assignmentManifestResponse
	if err := json.Unmarshal([]byte(toolResult.Content[0].Text), &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func newAssignmentManifestLocator(t *testing.T, server *httptest.Server) *Locator {
	t.Helper()
	table, err := NewRouteTable([]Route{{Operation: "compose_assignment_manifest", Backend: "tasks", Method: http.MethodPost, Path: "/v1/tasks/{task_id}/assignment-manifest", RequestAdapter: RequestAdapterMCPAssignmentManifestCompose, ResponseAdapter: ResponseAdapterMCPToolResultJSON}})
	if err != nil {
		t.Fatal(err)
	}
	locator, err := NewLocator([]config.BackendConfig{testBackend("tasks", server.URL), testBackend("guidance", server.URL), testBackend("knowledge", server.URL), testBackend("librarian", server.URL)}, table, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return locator
}

func newAssignmentManifestLocatorWithoutOptional(t *testing.T, server *httptest.Server) *Locator {
	t.Helper()
	table, err := NewRouteTable([]Route{{Operation: "compose_assignment_manifest", Backend: "tasks", Method: http.MethodPost, Path: "/v1/tasks/{task_id}/assignment-manifest", RequestAdapter: RequestAdapterMCPAssignmentManifestCompose, ResponseAdapter: ResponseAdapterMCPToolResultJSON}})
	if err != nil {
		t.Fatal(err)
	}
	locator, err := NewLocator([]config.BackendConfig{testBackend("tasks", server.URL), testBackend("guidance", server.URL)}, table, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return locator
}
