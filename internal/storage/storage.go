package storage

import (
	"context"
	"errors"
	"fmt"
	"voice-chat-api/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Storage struct {
	pool *pgxpool.Pool
}

func NewStorage(ctx context.Context, secrets *config.Secrets, cfg *config.Config) (*Storage, error) {
	const op = "Storage.NewStorage"

	if secrets.PostgresURL == "" {
		return nil, fmt.Errorf("%s: %w", op, errors.New("empty connection url"))
	}

	poolCfg, err := pgxpool.ParseConfig(secrets.PostgresURL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if cfg.Postgres.MaxConns > 0 {
		poolCfg.MaxConns = cfg.Postgres.MaxConns
	}
	if cfg.Postgres.HealthcheckPeriod > 0 {
		poolCfg.HealthCheckPeriod = cfg.Postgres.HealthcheckPeriod
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	s := &Storage{pool: pool}

	if err := s.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return s, nil
}

func (s *Storage) Close() error {
	if s == nil || s.pool == nil {
		return nil
	}
	s.pool.Close()
	return nil
}

func (s *Storage) Pool() *pgxpool.Pool {
	return s.pool
}

func (s *Storage) Ping(ctx context.Context) error {
	const op = "Storage.Ping"

	if s == nil || s.pool == nil {
		return fmt.Errorf("%s: %w", op, errors.New("nil pool"))
	}
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}
