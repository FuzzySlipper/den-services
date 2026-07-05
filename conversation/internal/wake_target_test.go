package conversation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRuntimeWakeTargetResolverEnrichesWakeableAgentMemberships(t *testing.T) {
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runtime/instances" {
			t.Fatalf("runtime path = %s, want /v1/runtime/instances", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer runtime-token" {
			t.Fatalf("runtime auth = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"instance_id": "den-mcp-runner@old",
				"profile_identity": "den-mcp-runner",
				"state": "active",
				"started_at": "2026-07-05T01:00:00Z",
				"last_heartbeat_at": "2026-07-05T01:00:00Z"
			},
			{
				"instance_id": "den-mcp-runner@new",
				"profile_identity": "den-mcp-runner",
				"state": "busy",
				"started_at": "2026-07-05T01:05:00Z",
				"last_heartbeat_at": "2026-07-05T01:10:00Z"
			},
			{
				"instance_id": "den-mcp-planner@stale",
				"profile_identity": "den-mcp-planner",
				"state": "stale",
				"started_at": "2026-07-05T01:00:00Z"
			}
		]`))
	}))
	t.Cleanup(runtimeServer.Close)

	runnerProfile := "den-mcp-runner"
	plannerProfile := "den-mcp-planner"
	memberships := []*ChannelMembership{
		{
			MemberType:       "agent",
			ProfileIdentity:  &runnerProfile,
			MembershipStatus: "active",
			WakePolicy:       "mentions_only",
		},
		{
			MemberType:       "agent",
			ProfileIdentity:  &runnerProfile,
			MembershipStatus: "active",
			WakePolicy:       "never",
		},
		{
			MemberType:       "agent",
			ProfileIdentity:  &plannerProfile,
			MembershipStatus: "active",
			WakePolicy:       "mentions_only",
		},
		{
			MemberType:       "human",
			MembershipStatus: "active",
			WakePolicy:       "mentions_only",
		},
	}

	resolver := NewRuntimeWakeTargetResolver(runtimeServer.URL, "runtime-token", time.Second)
	if err := resolver.ResolveWakeTargets(context.Background(), memberships); err != nil {
		t.Fatalf("ResolveWakeTargets() error = %v", err)
	}

	if memberships[0].WakeTarget == nil {
		t.Fatal("wakeable runner membership missing wake target")
	}
	if memberships[0].WakeTarget.Profile.String() != runnerProfile || memberships[0].WakeTarget.InstanceID.String() != "den-mcp-runner@new" {
		t.Fatalf("wake target = %+v", memberships[0].WakeTarget)
	}
	for index, membership := range memberships[1:] {
		if membership.WakeTarget != nil {
			t.Fatalf("membership %d wake target = %+v, want nil", index+1, membership.WakeTarget)
		}
	}
}

func TestNoopWakeTargetResolverLeavesMembershipsUnchanged(t *testing.T) {
	profile := "den-mcp-runner"
	memberships := []*ChannelMembership{{
		MemberType:       "agent",
		ProfileIdentity:  &profile,
		MembershipStatus: "active",
		WakePolicy:       "mentions_only",
	}}

	if err := (NoopWakeTargetResolver{}).ResolveWakeTargets(context.Background(), memberships); err != nil {
		t.Fatalf("ResolveWakeTargets() error = %v", err)
	}
	if memberships[0].WakeTarget != nil {
		t.Fatalf("WakeTarget = %+v, want nil", memberships[0].WakeTarget)
	}
}

func TestRuntimeWakeTargetResolverOmitsTargetsWhenRuntimeUnavailable(t *testing.T) {
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(runtimeServer.Close)

	profile := "den-mcp-runner"
	memberships := []*ChannelMembership{{
		MemberType:       "agent",
		ProfileIdentity:  &profile,
		MembershipStatus: "active",
		WakePolicy:       "mentions_only",
	}}

	resolver := NewRuntimeWakeTargetResolver(runtimeServer.URL, "runtime-token", time.Second)
	if err := resolver.ResolveWakeTargets(context.Background(), memberships); err != nil {
		t.Fatalf("ResolveWakeTargets() error = %v", err)
	}
	if memberships[0].WakeTarget != nil {
		t.Fatalf("WakeTarget = %+v, want nil", memberships[0].WakeTarget)
	}
}
