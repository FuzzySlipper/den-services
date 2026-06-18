package migration

import (
	"errors"
	"testing"
)

func TestNewRunnerRequiresPool(t *testing.T) {
	_, err := NewRunner(nil, nil)
	if !errors.Is(err, ErrMissingPool) {
		t.Fatalf("NewRunner() error = %v, want %v", err, ErrMissingPool)
	}
}
