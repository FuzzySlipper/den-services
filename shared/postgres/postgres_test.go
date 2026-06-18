package postgres

import (
	"context"
	"errors"
	"testing"
)

func TestConnectRequiresDatabaseURL(t *testing.T) {
	_, err := Connect(context.Background(), PoolConfig{})
	if !errors.Is(err, ErrMissingDatabaseURL) {
		t.Fatalf("Connect() error = %v, want %v", err, ErrMissingDatabaseURL)
	}
}
