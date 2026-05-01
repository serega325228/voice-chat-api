package repo

import (
	"context"
	"errors"
	"fmt"
	"time"
	storageErrors "voice-chat-api/internal/errors"
	"voice-chat-api/internal/models"
	"voice-chat-api/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type QuerierProvider interface {
	GetQuerier(ctx context.Context) storage.Querier
}

type TokenRepo struct {
	qp QuerierProvider
}

const tokenColumns = "id, token_hash, status, user_id, family_id, expires_at, created_at"

func NewTokenRepo(querierProvider QuerierProvider) *TokenRepo {
	return &TokenRepo{qp: querierProvider}
}

func (r *TokenRepo) GetByHash(ctx context.Context, tokenHash [32]byte) (models.RefreshToken, error) {
	const op = "TokenRepo.GetByHash"
	q := r.qp.GetQuerier(ctx)
	query := `
		SELECT ` + tokenColumns + `
		FROM tokens
		WHERE token_hash = $1
	`
	var token models.RefreshToken
	err := q.QueryRow(ctx, query, tokenHash).Scan(
		&token.ID,
		&token.TokenHash,
		&token.Status,
		&token.UserID,
		&token.FamilyID,
		&token.ExpiresAt,
		&token.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.RefreshToken{}, fmt.Errorf("%s: %w", op, storageErrors.ErrTokenNotFound)
		}
		return models.RefreshToken{}, fmt.Errorf("%s: %w", op, err)
	}

	return token, nil
}

func (r *TokenRepo) GetByHashForUpdate(ctx context.Context, tokenHash [32]byte) (models.RefreshToken, error) {
	const op = "TokenRepo.GetByHashForUpdate"
	q := r.qp.GetQuerier(ctx)
	query := `
		SELECT ` + tokenColumns + `
		FROM tokens
		WHERE token_hash = $1
		FOR UPDATE
	`
	var token models.RefreshToken
	err := q.QueryRow(ctx, query, tokenHash).Scan(
		&token.ID,
		&token.TokenHash,
		&token.Status,
		&token.UserID,
		&token.FamilyID,
		&token.ExpiresAt,
		&token.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.RefreshToken{}, fmt.Errorf("%s: %w", op, storageErrors.ErrTokenNotFound)
		}
		return models.RefreshToken{}, fmt.Errorf("%s: %w", op, err)
	}

	return token, nil
}

func (r *TokenRepo) GetByID(ctx context.Context, tokenID uuid.UUID) (models.RefreshToken, error) {
	const op = "TokenRepo.GetByID"
	q := r.qp.GetQuerier(ctx)
	query := `
		SELECT ` + tokenColumns + `
		FROM tokens
		WHERE id = $1
	`
	var token models.RefreshToken
	err := q.QueryRow(ctx, query, tokenID).Scan(
		&token.ID,
		&token.TokenHash,
		&token.Status,
		&token.UserID,
		&token.FamilyID,
		&token.ExpiresAt,
		&token.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.RefreshToken{}, fmt.Errorf("%s: %w", op, storageErrors.ErrTokenNotFound)
		}
		return models.RefreshToken{}, fmt.Errorf("%s: %w", op, err)
	}

	return token, nil
}

func (r *TokenRepo) Create(ctx context.Context, familyID, userID uuid.UUID, tokenHash [32]byte, status models.RefreshTokenStatus, created_at, expires_at time.Time) (uuid.UUID, error) {
	const op = "TokenRepo.Create"
	q := r.qp.GetQuerier(ctx)
	query := `
		INSERT INTO tokens
		(family_id, user_id, token_hash, status, created_at, expires_at) 
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`
	var id uuid.UUID
	err := q.QueryRow(ctx, query, familyID, userID, tokenHash, status, created_at, expires_at).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	return id, nil
}

func (r *TokenRepo) GetLatestByFamilyID(ctx context.Context, familyID uuid.UUID) (models.RefreshToken, error) {
	const op = "TokenRepo.GetLatestByFamilyID"
	q := r.qp.GetQuerier(ctx)
	query := `
		SELECT ` + tokenColumns + `
		FROM tokens
		WHERE family_id = $1
		ORDER BY expires_at DESC
		LIMIT 1;
	`
	var token models.RefreshToken
	err := q.QueryRow(ctx, query, familyID).Scan(
		&token.ID,
		&token.TokenHash,
		&token.Status,
		&token.UserID,
		&token.FamilyID,
		&token.ExpiresAt,
		&token.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.RefreshToken{}, fmt.Errorf("%s: %w", op, storageErrors.ErrTokenNotFound)
		}
		return models.RefreshToken{}, fmt.Errorf("%s: %w", op, err)
	}

	return token, nil
}

func (r *TokenRepo) ChangeFamilyStatus(ctx context.Context, familyID uuid.UUID, statusBefore, statusAfter models.RefreshTokenStatus) error {
	const op = "TokenRepo.ChangeFamilyStatus"
	q := r.qp.GetQuerier(ctx)
	query := `
		UPDATE tokens
		SET status = $1
		WHERE family_id = $2
		AND status = $3
	`
	_, err := q.Exec(ctx, query, statusAfter, familyID, statusBefore)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (r *TokenRepo) GetLatestByUserID(ctx context.Context, userID uuid.UUID) (models.RefreshToken, error) {
	const op = "TokenRepo.GetLatestByUserID"
	q := r.qp.GetQuerier(ctx)
	query := `
		SELECT ` + tokenColumns + `
		FROM tokens
		WHERE user_id = $1
		ORDER BY expires_at DESC
		LIMIT 1;
	`
	var token models.RefreshToken
	err := q.QueryRow(ctx, query, userID).Scan(
		&token.ID,
		&token.TokenHash,
		&token.Status,
		&token.UserID,
		&token.FamilyID,
		&token.ExpiresAt,
		&token.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.RefreshToken{}, fmt.Errorf("%s: %w", op, storageErrors.ErrTokenNotFound)
		}
		return models.RefreshToken{}, fmt.Errorf("%s: %w", op, err)
	}

	return token, nil
}

func (r *TokenRepo) ChangeStatus(ctx context.Context, tokenID uuid.UUID, status models.RefreshTokenStatus) error {
	const op = "TokenRepo.ChangeStatus"
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
