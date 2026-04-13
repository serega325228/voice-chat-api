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
	if secrets.PostgresURL == "" {
		return nil, errors.New("storage: empty connection url")
	}

	poolCfg, err := pgxpool.ParseConfig(secrets.PostgresURL)
	if err != nil {
		return nil, fmt.Errorf("storage: parse config: %w", err)
	}

	if cfg.Postgres.MaxConns > 0 {
		poolCfg.MaxConns = cfg.Postgres.MaxConns
	}
	if cfg.Postgres.HealthcheckPeriod > 0 {
		poolCfg.HealthCheckPeriod = cfg.Postgres.HealthcheckPeriod
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("storage: create pool: %w", err)
	}

	s := &Storage{pool: pool}

	if err := s.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("storage: ping db: %w", err)
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
	if s == nil || s.pool == nil {
		return errors.New("storage: nil pool")
	}
	return s.pool.Ping(ctx)
}
