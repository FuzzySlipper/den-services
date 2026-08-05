package handoff

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSetReplacesCurrentValueAndPreservesCallerMarkdown(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC)
	service := NewService(store, func() time.Time { return now })
	firstBody := "---\ndatetime: caller-owned\n---\n\nfirst"
	first, err := service.Set(context.Background(), SetHandoffRequest{Label: " den-services ", BodyMarkdown: firstBody})
	if err != nil {
		t.Fatalf("Set(first) error = %v", err)
	}
	if first.Revision() != 1 || first.BodyMarkdown() != firstBody || first.Label() != "den-services" {
		t.Fatalf("first = %#v", first)
	}
	now = now.Add(time.Minute)
	second, err := service.Set(context.Background(), SetHandoffRequest{Label: "den-services", BodyMarkdown: "second"})
	if err != nil {
		t.Fatalf("Set(second) error = %v", err)
	}
	if second.Revision() != 2 || second.CreatedAt() != first.CreatedAt() || !second.UpdatedAt().Equal(now) {
		t.Fatalf("second revision/times = %d %s %s", second.Revision(), second.CreatedAt(), second.UpdatedAt())
	}
	current, err := service.Get(context.Background(), "den-services")
	if err != nil || current.BodyMarkdown() != "second" || current.UpdatedBy() != "handoff-service" {
		t.Fatalf("Get() = %#v, %v", current, err)
	}
	for index, revision := range store.history["den-services"] {
		if revision.UpdatedBy() != "handoff-service" {
			t.Fatalf("history[%d].UpdatedBy() = %q, want handoff-service", index, revision.UpdatedBy())
		}
	}
	if len(store.history["den-services"]) != 2 {
		t.Fatalf("history count = %d, want 2", len(store.history["den-services"]))
	}
}

func TestSetValidatesLabelAndBody(t *testing.T) {
	service := NewService(newMemoryStore(), time.Now)
	tests := []struct {
		name    string
		request SetHandoffRequest
		want    error
	}{
		{name: "bad label", request: SetHandoffRequest{Label: "has spaces", BodyMarkdown: "body"}, want: ErrInvalidLabel},
		{name: "empty body", request: SetHandoffRequest{Label: "valid", BodyMarkdown: ""}, want: ErrMissingBody},
		{name: "large body", request: SetHandoffRequest{Label: "valid", BodyMarkdown: strings.Repeat("x", MaxBodyBytes+1)}, want: ErrBodyTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Set(context.Background(), test.request)
			if !errors.Is(err, test.want) {
				t.Fatalf("Set() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestConcurrentSetsProduceMonotonicRevisions(t *testing.T) {
	store := newMemoryStore()
	service := NewService(store, time.Now)
	const writers = 16
	var wait sync.WaitGroup
	wait.Add(writers)
	for index := range writers {
		go func() {
			defer wait.Done()
			if _, err := service.Set(context.Background(), SetHandoffRequest{Label: "campaign/test", BodyMarkdown: string(rune('a' + index))}); err != nil {
				t.Errorf("Set() error = %v", err)
			}
		}()
	}
	wait.Wait()
	current, err := service.Get(context.Background(), "campaign/test")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if current.Revision() != writers || len(store.history["campaign/test"]) != writers {
		t.Fatalf("revision/history = %d/%d, want %d/%d", current.Revision(), len(store.history["campaign/test"]), writers, writers)
	}
}
