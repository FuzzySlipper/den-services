package integration

import (
	"context"
	"testing"

	"den-services/shared/identity"
)

func TestMockRuntimeClient(t *testing.T) {
	client := NewMockRuntimeClient()
	instanceID := identity.AgentInstanceID("planner@host")
	client.SetAlive(instanceID, true)

	alive, err := client.IsAlive(context.Background(), instanceID)
	if err != nil {
		t.Fatalf("IsAlive() error = %v", err)
	}
	if !alive {
		t.Fatal("IsAlive() = false, want true")
	}
}

func TestMockDeliveryClient(t *testing.T) {
	client := NewMockDeliveryClient()
	request := DeliveryIntentRequest{
		TargetIdentity: identity.AgentIdentity{
			Profile:    identity.ProfileIdentity("planner"),
			InstanceID: identity.AgentInstanceID("planner@host"),
		},
		IdempotencyKey: "op:channel:planner:nonce",
	}

	if err := client.CreateIntent(context.Background(), request); err != nil {
		t.Fatalf("CreateIntent() error = %v", err)
	}
	if got := len(client.CreatedIntents()); got != 1 {
		t.Fatalf("len(CreatedIntents()) = %d, want 1", got)
	}
}
