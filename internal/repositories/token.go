package repo

import (
	"context"
	"fmt"
	"time"
	"voice-chat-api/internal/models"
	"voice-chat-api/internal/storage"

	"github.com/google/uuid"
)

type QuerierProvider interface {
	GetQuerier(ctx context.Context) storage.Querier
}

type TokenRepo struct {
	qp QuerierProvider
}

func NewTokenRepo(querierProvider QuerierProvider) *TokenRepo {
	return &TokenRepo{qp: querierProvider}
}

func (r *TokenRepo) GetByHash(ctx context.Context, tokenHash [32]byte) (models.RefreshToken, error) {
	const op = "token.GetByHash"
	q := r.qp.GetQuerier(ctx)
	query := `
		SELECT * 
		FROM tokens
		WHERE token_hash = $1
	`
	var token models.RefreshToken
	err := q.QueryRow(ctx, query, tokenHash).Scan(&token)
	if err != nil {
		return models.RefreshToken{}, fmt.Errorf("%s: %w", op, err)
	}

	return token, nil
}

func (r *TokenRepo) GetByID(ctx context.Context, tokenID uuid.UUID) (models.RefreshToken, error) {
	const op = "token.GetByID"
	q := r.qp.GetQuerier(ctx)
	query := `
		SELECT * 
		FROM tokens
		WHERE id = $1
	`
	var token models.RefreshToken
	err := q.QueryRow(ctx, query, tokenID).Scan(&token)
	if err != nil {
		return models.RefreshToken{}, fmt.Errorf("%s: %w", op, err)
	}

	return token, nil
}

func (r *TokenRepo) Create(ctx context.Context, userID uuid.UUID, tokenHash [32]byte, status models.RefreshTokenStatus, created_at, expires_at time.Time) (uuid.UUID, error) {
	const op = "token.Create"
	q := r.qp.GetQuerier(ctx)
	query := `
		INSERT INTO tokens
		(user_id, token_hash, status, created_at, expires_at) 
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`
	var id uuid.UUID
	err := q.QueryRow(ctx, query, userID, tokenHash, status, created_at, expires_at).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	return id, nil
}

func (r *TokenRepo) GetLatestByUserID(ctx context.Context, userID uuid.UUID) (models.RefreshToken, error) {
	const op = "token.GetByID"
	q := r.qp.GetQuerier(ctx)
	query := `
		SELECT *
		FROM tokens
		WHERE user_id = $1
		ORDER BY expires_at DESC
		LIMIT 1;
	`
	var token models.RefreshToken
	err := q.QueryRow(ctx, query, userID).Scan(&token)
	if err != nil {
		return models.RefreshToken{}, fmt.Errorf("%s: %w", op, err)
	}

	return token, nil
}

func (r *TokenRepo) ChangeStatus(ctx context.Context, tokenID uuid.UUID, status models.RefreshTokenStatus) error {
	const op = "token.ChangeStatus"
	q := r.qp.GetQuerier(ctx)
	query := `
		UPDATE tokens 
		SET status = $1
		WHERE id = $2
	`
	if _, err := q.Exec(ctx, query, status, tokenID); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}
