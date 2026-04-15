package repo

import (
	"context"
	"fmt"
	"voice-chat-api/internal/models"

	"github.com/google/uuid"
)

type UserRepo struct {
	qp QuerierProvider
}

func NewUserRepo(querierProvider QuerierProvider) *UserRepo {
	return &UserRepo{qp: querierProvider}
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (models.User, error) {
	const op = "user.GetByEmail"
	q := r.qp.GetQuerier(ctx)
	query := `
		SELECT *
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
		return models.User{}, fmt.Errorf("%s: %w", op, err)
	}

	return user, nil
}

func (r *UserRepo) GetByID(ctx context.Context, userID uuid.UUID) (models.User, error) {
	const op = "user.GetByID"
	q := r.qp.GetQuerier(ctx)
	query := `
		SELECT *
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
		return models.User{}, fmt.Errorf("%s: %w", op, err)
	}

	return user, nil
}

func (r *UserRepo) Create(ctx context.Context, username, email string, passHash []byte) (uuid.UUID, error) {
	const op = "user.Create"
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
		return uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	return uid, nil
}
