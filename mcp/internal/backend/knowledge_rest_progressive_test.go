package backend

import (
	"encoding/json"
	"net/http"
	"testing"

	"den-services/mcp/internal/config"
)

func TestProgressiveKnowledgeRESTAdaptersBuildBoundedRequests(t *testing.T) {
	backend := config.BackendConfig{BaseURL: "http://knowledge.test"}
	for _, test := range []struct {
		name      string
		operation string
		method    string
		path      string
		arguments string
		wantURL   string
		wantBody  string
	}{
		{"card", "den_knowledge_card", http.MethodGet, "/v1/knowledge/entries/{slug}/card", `{"slug":"a/b","include_archived":true}`, "http://knowledge.test/v1/knowledge/entries/a%2Fb/card?include_archived=true", ""},
		{"cards", "den_knowledge_cards", http.MethodPost, "/v1/knowledge/entries/cards", `{"slugs":["one","two"],"offset":1,"include_archived":true}`, "http://knowledge.test/v1/knowledge/entries/cards?offset=1", `{"slugs":["one","two"],"include_archived":true}`},
		{"section", "den_knowledge_read", http.MethodGet, "/v1/knowledge/entries/{slug}/read", `{"slug":"one","view":"section","section":"intro","known_revision":2,"known_digest":"abc","include_archived":true}`, "http://knowledge.test/v1/knowledge/entries/one/read?include_archived=true&known_digest=abc&known_revision=2&section=intro&view=section", ""},
		{"links", "den_knowledge_replace_links", http.MethodPut, "/v1/knowledge/entries/{slug}/links", `{"slug":"one","links":[{"to_slug":"two","kind":"related"}]}`, "http://knowledge.test/v1/knowledge/entries/one/links", `{"links":[{"to_slug":"two","kind":"related"}]}`},
		{"map", "den_knowledge_store_map", http.MethodPost, "/v1/knowledge/maps", `{"slug":"map","title":"Map","entries":[{"entry_slug":"one","position":0}]}`, "http://knowledge.test/v1/knowledge/maps", `{"slug":"map","title":"Map","entries":[{"entry_slug":"one","position":0}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := buildKnowledgeRESTRequest(t.Context(), backend, Route{Operation: test.operation, Method: test.method, Path: test.path}, ToolCall{Operation: test.operation, Arguments: json.RawMessage(test.arguments)})
			if err != nil {
				t.Fatal(err)
			}
			if request.URL.String() != test.wantURL {
				t.Fatalf("URL = %q, want %q", request.URL, test.wantURL)
			}
			if test.wantBody == "" {
				return
			}
			var want, got any
			if err := json.Unmarshal([]byte(test.wantBody), &want); err != nil {
				t.Fatal(err)
			}
			if err := json.NewDecoder(request.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if string(mustJSON(t, got)) != string(mustJSON(t, want)) {
				t.Fatalf("body = %s, want %s", mustJSON(t, got), mustJSON(t, want))
			}
		})
	}
}

func TestProgressiveKnowledgeRESTAdaptersRejectInvalidBounds(t *testing.T) {
	_, err := knowledgeRESTRequestBody("den_knowledge_cards", knowledgeToolArguments{Slugs: make([]string, 101)})
	if err == nil {
		t.Fatal("cards over bound succeeded")
	}
	_, err = knowledgeRESTURL("http://knowledge.test", Route{Operation: "den_knowledge_read", Path: "/v1/knowledge/entries/{slug}/read"}, knowledgeToolArguments{Slug: "one", View: "section"})
	if err == nil {
		t.Fatal("section without section id succeeded")
	}
	_, err = knowledgeRESTRequestBody("den_knowledge_replace_links", knowledgeToolArguments{Links: json.RawMessage(`[{"to_slug":"two","kind":"mentions"}]`)})
	if err == nil {
		t.Fatal("unknown link kind succeeded")
	}
}
