package integration

import (
	"testing"
	"time"
)

func TestFixedClock(t *testing.T) {
	start := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	clock := NewFixedClock(start)
	clock.Advance(5 * time.Minute)

	if got := clock.Now(); !got.Equal(start.Add(5 * time.Minute)) {
		t.Fatalf("Now() = %v, want %v", got, start.Add(5*time.Minute))
	}
}
