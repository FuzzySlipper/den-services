package gateway

import (
	"testing"

	sharedconfig "den-services/shared/config"
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
			SuccessorAuth:        testSuccessorAuth(),
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

func TestRouteTableRejectsSuccessorWithoutAuth(t *testing.T) {
	_, err := NewRouteTable([]routeFile{{
		Name:                 "delivery",
		PathPattern:          "/v1/delivery",
		LegacyUpstreamURL:    "http://legacy",
		SuccessorUpstreamURL: "http://successor",
	}})
	if err == nil {
		t.Fatal("NewRouteTable() error = nil, want missing successor auth error")
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
			SuccessorAuth:        testSuccessorAuth(),
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

func TestRouteTableRejectsMissingCallerAuthExpansion(t *testing.T) {
	_, err := NewRouteTableWithValues([]routeFile{{
		Name:              "observation-read",
		PathPattern:       "/v1/observation",
		Methods:           []string{"GET"},
		LegacyUpstreamURL: "http://legacy",
		CallerAuth:        callerAuthFile{BearerToken: "${MISSING_OBSERVATION_READ_TOKEN}"},
	}}, sharedconfig.FromMap(nil))
	if err == nil {
		t.Fatal("NewRouteTableWithValues() error = nil, want missing caller auth env error")
	}
}

func TestRouteTableUsesSuccessorCallerAuthOnlyForCanarySelection(t *testing.T) {
	table, err := NewRouteTableWithValuesAndDefaultAuth([]routeFile{{
		Name:                 "conversation-read-canary",
		PathPattern:          "/v1/conversation",
		Methods:              []string{httpMethodGet},
		LegacyUpstreamURL:    "http://legacy",
		SuccessorUpstreamURL: "http://conversation",
		SuccessorAuth:        testSuccessorAuth(),
		SuccessorCallerAuth:  callerAuthFile{BearerToken: "conversation-read-token"},
	}}, sharedconfig.FromMap(nil), CallerAuth{bearerToken: "gateway-default-token"})
	if err != nil {
		t.Fatalf("NewRouteTableWithValuesAndDefaultAuth() error = %v", err)
	}

	match, ok := table.Match(httpMethodGet, "/v1/conversation/channels", false)
	if !ok {
		t.Fatal("legacy fallback did not match")
	}
	if match.UsesSuccessor {
		t.Fatal("legacy fallback UsesSuccessor = true, want false")
	}
	if match.CallerAuth.bearerToken != "gateway-default-token" {
		t.Fatalf("legacy caller token = %q, want gateway default", match.CallerAuth.bearerToken)
	}

	match, ok = table.Match(httpMethodGet, "/v1/conversation/channels", true)
	if !ok {
		t.Fatal("successor canary did not match")
	}
	if !match.UsesSuccessor {
		t.Fatal("successor canary UsesSuccessor = false, want true")
	}
	if match.CallerAuth.bearerToken != "conversation-read-token" {
		t.Fatalf("successor caller token = %q, want conversation read", match.CallerAuth.bearerToken)
	}
}

func TestRouteTableRejectsSuccessorCallerAuthWithoutSuccessor(t *testing.T) {
	_, err := NewRouteTable([]routeFile{{
		Name:                "bad",
		PathPattern:         "/v1/conversation",
		LegacyUpstreamURL:   "http://legacy",
		SuccessorCallerAuth: callerAuthFile{BearerToken: "conversation-read-token"},
	}})
	if err == nil {
		t.Fatal("NewRouteTable() error = nil, want successor_caller_auth without successor error")
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

func TestRouteTableAllowsDisjointMethodsForSamePath(t *testing.T) {
	_, err := NewRouteTable([]routeFile{
		{Name: "conversation-read", PathPattern: "/v1/conversation", Methods: []string{httpMethodGet}, LegacyUpstreamURL: "http://legacy"},
		{Name: "conversation-write", PathPattern: "/v1/conversation", Methods: []string{httpMethodPost}, LegacyUpstreamURL: "http://legacy"},
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v, want nil", err)
	}
}

func testSuccessorAuth() upstreamAuthFile {
	return upstreamAuthFile{BearerToken: "successor-token"}
}

const (
	httpMethodGet  = "GET"
	httpMethodPost = "POST"
)
