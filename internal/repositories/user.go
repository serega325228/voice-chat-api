package repo

import (
	"context"
	"errors"
	"fmt"
	storageErrors "voice-chat-api/internal/errors"
	"voice-chat-api/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type UserRepo struct {
	qp QuerierProvider
}

const userColumns = "id, username, email, password_hash, created_at, updated_at"

func NewUserRepo(querierProvider QuerierProvider) *UserRepo {
	return &UserRepo{qp: querierProvider}
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	const op = "UserRepo.GetByEmail"
	q := r.qp.GetQuerier(ctx)
	query := `
		SELECT ` + userColumns + `
		FROM users
		WHERE email = $1
	`
	var user models.User
	err := q.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PassHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%s: %w", op, storageErrors.ErrUserNotFound)
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &user, nil
}

func (r *UserRepo) GetByID(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	const op = "UserRepo.GetByID"
	q := r.qp.GetQuerier(ctx)
	query := `
		SELECT ` + userColumns + `
		FROM users
		WHERE id = $1
	`
	var user models.User
	err := q.QueryRow(ctx, query, userID).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PassHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%s: %w", op, storageErrors.ErrUserNotFound)
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &user, nil
}

func (r *UserRepo) Create(ctx context.Context, username, email string, passHash []byte) (uuid.UUID, error) {
	const op = "UserRepo.Create"
	q := r.qp.GetQuerier(ctx)
	query := `
		INSERT INTO users
		(username, email, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id
	`
	var uid uuid.UUID
	err := q.QueryRow(ctx, query, username, email, passHash).Scan(&uid)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return uuid.Nil, fmt.Errorf("%s: %w", op, storageErrors.ErrUserExists)
		}
		return uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	return uid, nil
}
