package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Pooler interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Ping(ctx context.Context) error
}

type txKey struct{}

type Transactor struct {
	storage *Storage
}

func NewTransactor(storage *Storage) *Transactor {
	return &Transactor{storage: storage}
}

func (t *Transactor) WithTx(
	ctx context.Context,
	fn func(ctx context.Context) error,
) error {
	const op = "Transactor.WithTx"

	if _, ok := t.txFromContext(ctx); ok {
		return fn(ctx)
	}

	tx, err := t.storage.Pool().Begin(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	txCtx := t.contextWithTx(ctx, tx)

	if err := fn(txCtx); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if err := tx.Commit(txCtx); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (t *Transactor) contextWithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

func (t *Transactor) txFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}

func (t *Transactor) GetQuerier(ctx context.Context) Querier {
	if tx, ok := t.txFromContext(ctx); ok {
		return tx
	}
	return t.storage.Pool()
}
