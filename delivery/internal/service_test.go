package delivery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"den-services/shared/identity"
)

func TestStaleRuntimeCannotClaimFreshWork(t *testing.T) {
	store := newMemoryIntentStore(t)
	runtime := &memoryRuntimeChecker{alive: false}
	service := NewIntentService(store, runtime, fixedClock(), 5*time.Minute, time.Hour, 5*time.Minute, 30*time.Minute)
	intent := store.mustCreateIntent(t, IntentStatePending)

	_, err := service.Claim(context.Background(), intent.ID(), ClaimRequest{
		ClaimToken: "token",
		ClaimedBy:  testIdentity(),
	})
	if !errors.Is(err, ErrRuntimeNotAlive) {
		t.Fatalf("Claim() error = %v, want %v", err, ErrRuntimeNotAlive)
	}
	if intent.State() != IntentStateExpired {
		t.Fatalf("state = %s, want %s", intent.State(), IntentStateExpired)
	}
}

func TestDuplicateClaimRejected(t *testing.T) {
	store := newMemoryIntentStore(t)
	runtime := &memoryRuntimeChecker{alive: true}
	service := NewIntentService(store, runtime, fixedClock(), 5*time.Minute, time.Hour, 5*time.Minute, 30*time.Minute)
	intent := store.mustCreateIntent(t, IntentStatePending)

	if _, err := service.Claim(context.Background(), intent.ID(), ClaimRequest{ClaimToken: "one", ClaimedBy: testIdentity()}); err != nil {
		t.Fatalf("first Claim() error = %v", err)
	}
	_, err := service.Claim(context.Background(), intent.ID(), ClaimRequest{ClaimToken: "two", ClaimedBy: testIdentity()})
	if !errors.Is(err, ErrIntentAlreadyClaimed) {
		t.Fatalf("second Claim() error = %v, want %v", err, ErrIntentAlreadyClaimed)
	}
}

func TestTerminalIntentCannotExecuteAgain(t *testing.T) {
	store := newMemoryIntentStore(t)
	runtime := &memoryRuntimeChecker{alive: true}
	service := NewIntentService(store, runtime, fixedClock(), 5*time.Minute, time.Hour, 5*time.Minute, 30*time.Minute)
	intent := store.mustCreateIntent(t, IntentStateCompleted)

	_, err := service.Claim(context.Background(), intent.ID(), ClaimRequest{ClaimToken: "token", ClaimedBy: testIdentity()})
	if !errors.Is(err, ErrIntentAlreadyCompleted) {
		t.Fatalf("Claim() error = %v, want %v", err, ErrIntentAlreadyCompleted)
	}
}

func TestReaperTransitions(t *testing.T) {
	store := newMemoryIntentStore(t)
	service := NewIntentService(store, &memoryRuntimeChecker{alive: false}, fixedClock(), 5*time.Minute, time.Hour, 5*time.Minute, 30*time.Minute)
	pending := store.mustCreateIntent(t, IntentStatePending)
	pending.createdAt = fixedClock()().Add(-6 * time.Minute)
	running := store.mustCreateIntent(t, IntentStateRunning)
	claimedAt := fixedClock()().Add(-31 * time.Minute)
	claimedBy := testIdentity()
	running.claimedAt = &claimedAt
	running.claimedBy = &claimedBy

	expired, failed, err := service.Reap(context.Background())
	if err != nil {
		t.Fatalf("Reap() error = %v", err)
	}
	if expired != 1 || failed != 1 {
		t.Fatalf("Reap() expired=%d failed=%d, want 1/1", expired, failed)
	}
	if pending.State() != IntentStateExpired || running.State() != IntentStateFailed {
		t.Fatalf("states pending=%s running=%s", pending.State(), running.State())
	}
}

func TestReaperKeepsRunningIntentForAliveRuntime(t *testing.T) {
	store := newMemoryIntentStore(t)
	service := NewIntentService(store, &memoryRuntimeChecker{alive: true}, fixedClock(), 5*time.Minute, time.Hour, 5*time.Minute, 30*time.Minute)
	running := store.mustCreateIntent(t, IntentStateRunning)
	claimedAt := fixedClock()().Add(-31 * time.Minute)
	claimedBy := testIdentity()
	running.claimedAt = &claimedAt
	running.claimedBy = &claimedBy

	expired, failed, err := service.Reap(context.Background())
	if err != nil {
		t.Fatalf("Reap() error = %v", err)
	}
	if expired != 0 || failed != 0 {
		t.Fatalf("Reap() expired=%d failed=%d, want 0/0", expired, failed)
	}
	if running.State() != IntentStateRunning {
		t.Fatalf("state = %s, want %s", running.State(), IntentStateRunning)
	}
}

func TestReplayCannotCreateFreshExecutionWithInvalidKeyScope(t *testing.T) {
	service := NewIntentService(newMemoryIntentStore(t), &memoryRuntimeChecker{alive: true}, fixedClock(), 5*time.Minute, time.Hour, 5*time.Minute, 30*time.Minute)

	_, err := service.Create(context.Background(), CreateIntentRequest{
		TargetIdentity: testIdentity(),
		IdempotencyKey: "op:1:other-profile:nonce",
	})
	if !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("Create() error = %v, want %v", err, ErrInvalidIntent)
	}
}

func TestCreateRejectsTTLAboveConfiguredMaximum(t *testing.T) {
	service := NewIntentService(newMemoryIntentStore(t), &memoryRuntimeChecker{alive: true}, fixedClock(), 5*time.Minute, 10*time.Minute, 5*time.Minute, 30*time.Minute)
	ttlSeconds := int64(601)

	_, err := service.Create(context.Background(), CreateIntentRequest{
		TargetIdentity: testIdentity(),
		IdempotencyKey: "op:42:planner:max-ttl",
		TTLSeconds:     &ttlSeconds,
	})
	if !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("Create() error = %v, want %v", err, ErrInvalidIntent)
	}
}

func TestConcurrentClaimOnlyOneSucceeds(t *testing.T) {
	store := newMemoryIntentStore(t)
	runtime := &memoryRuntimeChecker{alive: true}
	service := NewIntentService(store, runtime, fixedClock(), 5*time.Minute, time.Hour, 5*time.Minute, 30*time.Minute)
	intent := store.mustCreateIntent(t, IntentStatePending)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, token := range []string{"one", "two"} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			_, err := service.Claim(context.Background(), intent.ID(), ClaimRequest{ClaimToken: token, ClaimedBy: testIdentity()})
			errs <- err
		}(token)
	}
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		if errors.Is(err, ErrIntentAlreadyClaimed) {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}
}

type memoryRuntimeChecker struct {
	alive bool
	err   error
}

func (c *memoryRuntimeChecker) IsAlive(_ context.Context, _ identity.AgentInstanceID) (bool, error) {
	if c.err != nil {
		return false, c.err
	}
	return c.alive, nil
}

func fixedClock() func() time.Time {
	return func() time.Time {
		return time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	}
}

func testIdentity() identity.AgentIdentity {
	return identity.AgentIdentity{
		Profile:    identity.ProfileIdentity("planner"),
		InstanceID: identity.AgentInstanceID("planner@den-srv"),
	}
}
