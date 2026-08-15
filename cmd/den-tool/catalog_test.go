package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestParseCatalogValidatesAndSortsStableIDs(t *testing.T) {
	catalog := Catalog{
		Version: "test",
		Tools: []Tool{
			testTool("zeta", RiskRead),
			testTool("alpha", RiskWrite),
		},
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	parsed, err := ParseCatalog(data)
	if err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	if got := parsed.Tools[0].ID; got != "alpha" {
		t.Fatalf("first tool id = %q, want alpha", got)
	}
	if got := parsed.Tools[1].ID; got != "zeta" {
		t.Fatalf("second tool id = %q, want zeta", got)
	}

	overrideRoot := filepath.Join(t.TempDir(), "repos")
	resolved, err := ResolveCatalog(parsed, overrideRoot)
	if err != nil {
		t.Fatalf("resolve catalog: %v", err)
	}
	wantRoot := filepath.Join(overrideRoot, "example")
	if got := resolved.Tools[0].Root; got != wantRoot {
		t.Fatalf("resolved root = %q, want %q", got, wantRoot)
	}
}

func TestCatalogValidationRejectsDuplicateIDsRiskAndEmptyArgv(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Catalog)
		wantErr string
	}{
		{
			name: "duplicate ids",
			mutate: func(catalog *Catalog) {
				catalog.Tools[1].ID = catalog.Tools[0].ID
			},
			wantErr: "duplicate tool id",
		},
		{
			name: "invalid risk",
			mutate: func(catalog *Catalog) {
				catalog.Tools[0].Risk = Risk("maybe")
			},
			wantErr: "invalid risk",
		},
		{
			name: "empty argv",
			mutate: func(catalog *Catalog) {
				catalog.Tools[0].Argv = nil
			},
			wantErr: "non-empty argv",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			catalog := Catalog{Version: "test", Tools: []Tool{testTool("alpha", RiskRead), testTool("beta", RiskRead)}}
			test.mutate(&catalog)
			err := catalog.Validate()
			if err == nil || !contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestSearchIsCaseInsensitiveAndRequiresAllTerms(t *testing.T) {
	catalog := Catalog{
		Version: "test",
		Tools: []Tool{
			testTool("den-web.test", RiskRead),
			testTool("den-services.go-test", RiskRead),
		},
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("validate catalog: %v", err)
	}
	result := catalog.Search([]string{"DEN-WEB", "TEST"})
	if len(result.Tools) != 1 || result.Tools[0].ID != "den-web.test" {
		t.Fatalf("search result = %#v, want den-web.test", result.Tools)
	}
	if result := catalog.Search([]string{"missing"}); len(result.Tools) != 0 {
		t.Fatalf("missing search result = %#v, want empty", result.Tools)
	}
}

func TestEmbeddedCatalogHasBothReposAndRequiredRecords(t *testing.T) {
	catalog, err := ParseCatalog(embeddedCatalog)
	if err != nil {
		t.Fatalf("parse embedded catalog: %v", err)
	}
	seen := map[string]bool{}
	for _, tool := range catalog.Tools {
		seen[tool.Repo] = true
		if len(tool.Argv) == 0 || len(tool.Requirements) == 0 || len(tool.Examples) == 0 {
			t.Fatalf("tool %q is missing explicit invocation metadata", tool.ID)
		}
	}
	if !seen["den-services"] || !seen["den-web"] {
		t.Fatalf("catalog repos = %#v, want den-services and den-web", seen)
	}
}

func TestEmbeddedCatalogIncludesCompleteBoardCommandFamily(t *testing.T) {
	catalog, err := ParseCatalog(embeddedCatalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"board.create-post", "board.list-posts", "board.get-post", "board.search",
		"board.create-comment", "board.list-comments", "board.get-comment",
		"board.comment-path", "board.purge-post", "board.purge-comment",
	} {
		if _, found := catalog.Find(id); !found {
			t.Errorf("catalog is missing %s", id)
		}
	}
}

func testTool(id string, risk Risk) Tool {
	return Tool{
		ID:               id,
		Description:      "test tool",
		Tags:             []string{"test"},
		Repo:             "example",
		Root:             "/home/dev/example",
		WorkingDirectory: "/home/dev/example",
		Argv:             []string{"echo", "hello"},
		Risk:             risk,
		Source:           "test",
		Requirements:     []string{"echo"},
		Examples:         []string{"example"},
	}
}

func contains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
