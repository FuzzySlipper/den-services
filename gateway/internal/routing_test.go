package gateway

import (
	"testing"
)

func TestRouteTableMatchesLongestPrefix(t *testing.T) {
	table, err := NewRouteTable([]routeFile{
		{Name: "all", PathPattern: "/", LegacyUpstreamURL: "http://legacy"},
		{Name: "api", PathPattern: "/api", LegacyUpstreamURL: "http://api-legacy"},
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}

	match, ok := table.Match("GET", "/api/channels", false)
	if !ok {
		t.Fatal("Match() ok = false")
	}
	if match.Target.String() != "http://api-legacy" {
		t.Fatalf("target = %s, want api legacy", match.Target.String())
	}
}

func TestRouteTableUsesSuccessorOnlyWhenHeaderCanSelectConfiguredRoute(t *testing.T) {
	table, err := NewRouteTable([]routeFile{
		{
			Name:                 "delivery",
			PathPattern:          "/v1/delivery",
			LegacyUpstreamURL:    "http://legacy",
			SuccessorUpstreamURL: "http://successor",
		},
		{Name: "all", PathPattern: "/", LegacyUpstreamURL: "http://legacy"},
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}

	match, ok := table.Match("POST", "/v1/delivery/intents", true)
	if !ok {
		t.Fatal("Match() ok = false")
	}
	if !match.UsesSuccessor {
		t.Fatal("UsesSuccessor = false, want true")
	}
	if match.Target.String() != "http://successor" {
		t.Fatalf("successor target = %s, want http://successor", match.Target.String())
	}

	match, ok = table.Match("GET", "/api/legacy", true)
	if !ok {
		t.Fatal("Match() ok = false")
	}
	if match.UsesSuccessor {
		t.Fatal("UsesSuccessor = true, want false")
	}
	if match.Target.String() != "http://legacy" {
		t.Fatalf("legacy target = %s, want http://legacy", match.Target.String())
	}
}

func TestRouteTableRejectsMissingLegacyUpstream(t *testing.T) {
	_, err := NewRouteTable([]routeFile{{Name: "bad", PathPattern: "/"}})
	if err == nil {
		t.Fatal("NewRouteTable() error = nil, want error")
	}
}

func TestRouteTableMatchesConfiguredMethods(t *testing.T) {
	table, err := NewRouteTable([]routeFile{
		{Name: "observation-read", PathPattern: "/v1/observation", Methods: []string{"GET"}, LegacyUpstreamURL: "http://legacy"},
		{Name: "observation-write", PathPattern: "/v1/observation/activity-events", Methods: []string{"POST"}, LegacyUpstreamURL: "http://legacy"},
		{Name: "all", PathPattern: "/", LegacyUpstreamURL: "http://all"},
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}

	match, ok := table.Match(httpMethodGet, "/v1/observation/lane", false)
	if !ok {
		t.Fatal("GET observation route did not match")
	}
	if match.Target.String() != "http://legacy" {
		t.Fatalf("GET target = %s, want observation route", match.Target.String())
	}

	match, ok = table.Match(httpMethodPost, "/v1/observation/activity-events", false)
	if !ok {
		t.Fatal("POST observation route did not match")
	}
	if match.Target.String() != "http://legacy" {
		t.Fatalf("POST target = %s, want observation write route", match.Target.String())
	}

	match, ok = table.Match(httpMethodPost, "/v1/observation/lane", false)
	if !ok {
		t.Fatal("POST unmatched observation route should fall through")
	}
	if match.Target.String() != "http://all" {
		t.Fatalf("fallback target = %s, want http://all", match.Target.String())
	}
}

func TestRouteTableUsesAlwaysSuccessorWithoutMigrationHeader(t *testing.T) {
	table, err := NewRouteTable([]routeFile{
		{
			Name:                 "observation-read",
			PathPattern:          "/v1/observation",
			Methods:              []string{"GET"},
			LegacyUpstreamURL:    "http://legacy",
			SuccessorUpstreamURL: "http://observation",
			SuccessorMode:        "always",
		},
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}

	match, ok := table.Match(httpMethodGet, "/v1/observation/lane", false)
	if !ok {
		t.Fatal("Match() ok = false")
	}
	if !match.UsesSuccessor {
		t.Fatal("UsesSuccessor = false, want true")
	}
	if match.Target.String() != "http://observation" {
		t.Fatalf("target = %s, want http://observation", match.Target.String())
	}
}

func TestRouteTableProjectTaskTemplateDoesNotCatchOtherProjectReads(t *testing.T) {
	table, err := NewRouteTable([]routeFile{
		{
			Name:                 "tasks-reads-and-streams",
			PathPattern:          "/v1/projects/{project_id}/tasks",
			Methods:              []string{httpMethodGet},
			LegacyUpstreamURL:    "http://legacy",
			SuccessorUpstreamURL: "http://tasks",
			SuccessorMode:        "always",
		},
		{Name: "all", PathPattern: "/", LegacyUpstreamURL: "http://legacy"},
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}

	successorPaths := []string{
		"/v1/projects/den-services/tasks",
		"/v1/projects/den-services/tasks/changes",
		"/v1/projects/den-services/tasks/changes/stream",
		"/v1/projects/den-services/tasks/4197",
	}
	for _, path := range successorPaths {
		match, ok := table.Match(httpMethodGet, path, false)
		if !ok {
			t.Fatalf("%s did not match", path)
		}
		if !match.UsesSuccessor {
			t.Fatalf("%s UsesSuccessor = false, want true", path)
		}
		if match.Target.String() != "http://tasks" {
			t.Fatalf("%s target = %s, want http://tasks", path, match.Target.String())
		}
	}

	legacyPaths := []string{
		"/v1/projects/den-services",
		"/v1/projects/den-services/documents",
		"/v1/projects/den-services/messages",
		"/v1/projects/den-services/librarian/query",
	}
	for _, path := range legacyPaths {
		match, ok := table.Match(httpMethodGet, path, false)
		if !ok {
			t.Fatalf("%s did not match fallback", path)
		}
		if match.UsesSuccessor {
			t.Fatalf("%s UsesSuccessor = true, want false", path)
		}
		if match.Target.String() != "http://legacy" {
			t.Fatalf("%s target = %s, want http://legacy", path, match.Target.String())
		}
	}
}

func TestRouteTableRuntimeRouteUsesAlwaysSuccessor(t *testing.T) {
	table, err := NewRouteTable([]routeFile{{
		Name:                 "runtime-instances",
		PathPattern:          "/v1/runtime/instances",
		Methods:              []string{httpMethodGet, httpMethodPost},
		LegacyUpstreamURL:    "http://legacy",
		SuccessorUpstreamURL: "http://runtime",
		SuccessorMode:        "always",
	}})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}

	match, ok := table.Match(httpMethodPost, "/v1/runtime/instances/agent-1/heartbeat", false)
	if !ok {
		t.Fatal("runtime heartbeat route did not match")
	}
	if !match.UsesSuccessor {
		t.Fatal("runtime route UsesSuccessor = false, want true")
	}
	if match.Target.String() != "http://runtime" {
		t.Fatalf("target = %s, want http://runtime", match.Target.String())
	}
}

func TestRouteTableRejectsDuplicateRouteNames(t *testing.T) {
	_, err := NewRouteTable([]routeFile{
		{Name: "duplicate", PathPattern: "/v1/first", LegacyUpstreamURL: "http://legacy"},
		{Name: "duplicate", PathPattern: "/v1/second", LegacyUpstreamURL: "http://legacy"},
	})
	if err == nil {
		t.Fatal("NewRouteTable() error = nil, want duplicate route name error")
	}
}

func TestRouteTableRejectsOverlappingExactPathAndMethod(t *testing.T) {
	_, err := NewRouteTable([]routeFile{
		{Name: "first", PathPattern: "/v1/observation", Methods: []string{httpMethodGet}, LegacyUpstreamURL: "http://legacy"},
		{Name: "second", PathPattern: "/v1/observation", Methods: []string{httpMethodGet}, LegacyUpstreamURL: "http://legacy"},
	})
	if err == nil {
		t.Fatal("NewRouteTable() error = nil, want overlapping method error")
	}
}

func TestRouteTableRewritesConfiguredNonTemplatedPrefix(t *testing.T) {
	table, err := NewRouteTable([]routeFile{{
		Name:                 "widgets",
		PathPattern:          "/v1/widgets",
		Methods:              []string{httpMethodPost},
		LegacyUpstreamURL:    "http://legacy",
		SuccessorUpstreamURL: "http://widgets",
		SuccessorMode:        string(SuccessorModeAlways),
		PathRewrite: pathRewriteFile{
			FromPrefix: "/v1/widgets",
			ToPrefix:   "/widgets",
		},
	}})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}

	match, ok := table.Match(httpMethodPost, "/v1/widgets/compare", false)
	if !ok {
		t.Fatal("Match() ok = false")
	}
	if got := match.PathRewrite.Apply("/v1/widgets/compare"); got != "/widgets/compare" {
		t.Fatalf("rewritten path = %q, want /widgets/compare", got)
	}
	if _, ok := table.Match(httpMethodPost, "/v1/widgets-extra/compare", false); ok {
		t.Fatal("near-prefix path unexpectedly matched")
	}
}

func TestRouteTableRejectsInvalidPathRewrite(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		rewrite pathRewriteFile
	}{
		{
			name:    "missing destination",
			pattern: "/v1/widgets",
			rewrite: pathRewriteFile{FromPrefix: "/v1/widgets"},
		},
		{
			name:    "prefix does not match route",
			pattern: "/v1/widgets",
			rewrite: pathRewriteFile{FromPrefix: "/v1/contracts", ToPrefix: "/widgets"},
		},
		{
			name:    "templated route",
			pattern: "/v1/projects/{project_id}/contracts",
			rewrite: pathRewriteFile{FromPrefix: "/v1/projects/{project_id}/contracts", ToPrefix: "/contracts"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewRouteTable([]routeFile{{
				Name:              "invalid-rewrite",
				PathPattern:       testCase.pattern,
				LegacyUpstreamURL: "http://legacy",
				PathRewrite:       testCase.rewrite,
			}})
			if err == nil {
				t.Fatal("NewRouteTable() error = nil, want path rewrite validation error")
			}
		})
	}
}

func TestRouteTableAllowsDisjointMethodsForSamePath(t *testing.T) {
	_, err := NewRouteTable([]routeFile{
		{Name: "observation-read", PathPattern: "/v1/observation", Methods: []string{httpMethodGet}, LegacyUpstreamURL: "http://legacy"},
		{Name: "observation-write", PathPattern: "/v1/observation", Methods: []string{httpMethodPost}, LegacyUpstreamURL: "http://legacy"},
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v, want nil", err)
	}
}

const (
	httpMethodGet  = "GET"
	httpMethodPost = "POST"
)
