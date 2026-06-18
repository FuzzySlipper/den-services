package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PoolConfig struct {
	DatabaseURL string
}

func Connect(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	if cfg.DatabaseURL == "" {
		return nil, ErrMissingDatabaseURL
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("creating postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}
	return pool, nil
}

func MustConnect(url string) *pgxpool.Pool {
	pool, err := Connect(context.Background(), PoolConfig{DatabaseURL: url})
	if err != nil {
		panic(fmt.Errorf("connecting postgres: %w", err)) //nolint:forbidigo
	}
	return pool
}
