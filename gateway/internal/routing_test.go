package gateway

import "testing"

func TestRouteTableMatchesLongestPrefix(t *testing.T) {
	table, err := NewRouteTable([]routeFile{
		{Name: "all", PathPattern: "/", LegacyUpstreamURL: "http://legacy"},
		{Name: "api", PathPattern: "/api", LegacyUpstreamURL: "http://api-legacy"},
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}

	match, ok := table.Match("/api/channels", false)
	if !ok {
		t.Fatal("Match() ok = false")
	}
	if match.Target.String() != "http://api-legacy" {
		t.Fatalf("target = %s, want api legacy", match.Target.String())
	}
}

func TestRouteTableUsesSuccessorOnlyWhenHeaderCanSelectConfiguredRoute(t *testing.T) {
	table, err := NewRouteTable([]routeFile{
		{Name: "delivery", PathPattern: "/v1/delivery", LegacyUpstreamURL: "http://legacy", SuccessorUpstreamURL: "http://successor"},
		{Name: "all", PathPattern: "/", LegacyUpstreamURL: "http://legacy"},
	})
	if err != nil {
		t.Fatalf("NewRouteTable() error = %v", err)
	}

	match, ok := table.Match("/v1/delivery/intents", true)
	if !ok {
		t.Fatal("Match() ok = false")
	}
	if !match.UsesSuccessor {
		t.Fatal("UsesSuccessor = false, want true")
	}
	if match.Target.String() != "http://successor" {
		t.Fatalf("successor target = %s, want http://successor", match.Target.String())
	}

	match, ok = table.Match("/api/legacy", true)
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
