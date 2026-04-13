package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"time"
	storageErrors "voice-chat-api/internal/errors"
	"voice-chat-api/internal/lib/jwt"
	"voice-chat-api/internal/lib/logger"
	"voice-chat-api/internal/models"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	log           *slog.Logger
	userProvider  UserProvider
	tokenProvider TokenProvider
	txProvider    TxProvider
	accessTTL     time.Duration
	refreshTTL    time.Duration
	tokenSecret   string
}

type TokenProvider interface {
	Create(
		ctx context.Context,
		userID uuid.UUID,
		tokenHash [32]byte,
		status models.RefreshTokenStatus,
		created_at,
		expires_at time.Time,
	) (uuid.UUID, error)
	GetByHash(ctx context.Context, tokenHash [32]byte) (models.RefreshToken, error)
	GetByID(ctx context.Context, tokenID uuid.UUID) (models.RefreshToken, error)
	GetLatestByUserID(ctx context.Context, userID uuid.UUID) (models.RefreshToken, error)
	ChangeStatus(ctx context.Context, tokenID uuid.UUID, status models.RefreshTokenStatus) error
}

type UserProvider interface {
	GetByEmail(ctx context.Context, email string) (models.User, error)
	GetByID(ctx context.Context, userID uuid.UUID) (models.User, error)
	Create(
		ctx context.Context,
		username string,
		email string,
		passHash []byte,
	) (uuid.UUID, error)
}

type TxProvider interface {
	WithTx(
		ctx context.Context,
		fn func(ctx context.Context) error,
	) error
}

var (
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrUserAlreadyExists   = errors.New("user already exists")
	ErrTokenAlreadyRotated = errors.New("token already is rotated")
)

func NewAuthService(
	log *slog.Logger,
	userProvider UserProvider,
	tokenProvider TokenProvider,
	txProvider TxProvider,
	accessTTL time.Duration,
	refreshTTL time.Duration,
	tokenSecret string,
) *AuthService {
	return &AuthService{
		userProvider:  userProvider,
		tokenProvider: tokenProvider,
		txProvider:    txProvider,
		log:           log,
		accessTTL:     accessTTL,
		refreshTTL:    refreshTTL,
		tokenSecret:   tokenSecret,
	}
}

func (a *AuthService) Login(
	ctx context.Context,
	email string,
	password string,
) (string, string, error) {
	const op = "auth.Login"

	log := a.log.With(
		slog.String("op", op),
	)

	log.Info("login user")

	user, err := a.userProvider.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, storageErrors.ErrUserNotFound) {
			log.Warn("user not found", logger.Err(err))
			return "", "", fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
		}
		log.Error("failed to get user", logger.Err(err))
		return "", "", fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
	}

	if err := bcrypt.CompareHashAndPassword(user.PassHash, []byte(password)); err != nil {
		log.Info("invalid credentials", logger.Err(err))
		return "", "", fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
	}

	log.Info("user logged is successfully")

	oldRefresh, err := a.tokenProvider.GetLatestByUserID(ctx, user.ID)
	if err != nil {
		return "", "", err
	}

	accessToken, refreshToken, err := a.Refresh(ctx, oldRefresh.TokenHash)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func (a *AuthService) RegisterUser(
	ctx context.Context,
	username string,
	email string,
	password string,
) (string, string, error) {
	const op = "auth.RegisterNewUser"

	log := a.log.With(
		slog.String("op", op),
	)

	log.Info("registering new user")

	_, err := a.userProvider.GetByEmail(ctx, email)
	if err == nil {
		log.Error("email already registered")
		return "", "", fmt.Errorf("%s: %w", op, ErrUserAlreadyExists)
	}

	passHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Error("failed to generate password hash", logger.Err(err))
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	userID, err := a.userProvider.Create(ctx, username, email, passHash)
	if err != nil {
		if errors.Is(err, ErrUserAlreadyExists) {
			log.Error("email already registered")
			return "", "", fmt.Errorf("%s: %w", op, ErrUserAlreadyExists)
		}
		log.Error("failed to save user", logger.Err(err))
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	log.Info("new user registered")

	_, refreshToken, err := a.CreateRefreshToken(ctx, userID)
	if err != nil {
		log.Error("failed to create refresh token", logger.Err(err))
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	log.Info("refresh token created")

	accessToken, err := jwt.NewAccessToken(userID, username, a.accessTTL, a.tokenSecret)
	if err != nil {
		log.Error("failed to create access token", logger.Err(err))
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	return accessToken, refreshToken, nil
}

func (a *AuthService) CreateRefreshToken(ctx context.Context, userID uuid.UUID) (uuid.UUID, string, error) {
	const op = "auth.CreateRefreshToken"

	log := a.log.With(
		slog.String("op", op),
	)

	log.Info("creating new refresh token")

	tokenString, iat, exp, err := jwt.NewRefreshToken(userID, a.refreshTTL, a.tokenSecret)
	if err != nil {
		log.Error("failed to generate refresh token", logger.Err(err))
		return uuid.Nil, "", fmt.Errorf("%s: %w", op, err)
	}

	tokenHash := sha256.Sum256([]byte(tokenString))

	tokenID, err := a.tokenProvider.Create(ctx, userID, tokenHash, models.Active, iat, exp)
	if err != nil {
		log.Error("failed to save token", logger.Err(err))
		return uuid.Nil, "", fmt.Errorf("%s: %w", op, err)
	}

	return tokenID, tokenString, nil
}

func (a *AuthService) MarkRefreshTokenAsRotated(ctx context.Context, token models.RefreshToken) error {
	const op = "auth.MarkRefreshTokenAsRotated"

	log := a.log.With(
		slog.String("op", op),
	)

	log.Info("mark refresh token as rotated")

	if token.Status == models.Rotated {
		log.Warn("token already is rotated")
		return fmt.Errorf("%s: %w", op, ErrTokenAlreadyRotated)
	}

	if err := a.tokenProvider.ChangeStatus(ctx, token.ID, models.Rotated); err != nil {
		log.Error("failed to change token status", logger.Err(err))
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (a *AuthService) Refresh(ctx context.Context, tokenHash [32]byte) (string, string, error) {
	const op = "auth.Refresh"

	log := a.log.With(
		slog.String("op", op),
	)

	var accessToken, refreshToken string

	err := a.txProvider.WithTx(ctx, func(ctx context.Context) error {
		latestRefresh, err := a.tokenProvider.GetByHash(ctx, tokenHash)
		if err != nil {
			log.Error("failed find latest token by hash", logger.Err(err))
			return err
		}

		if err := a.MarkRefreshTokenAsRotated(ctx, latestRefresh); err != nil {
			log.Error("failed mark token as rotated", logger.Err(err))
			return err
		}

		user, err := a.userProvider.GetByID(ctx, latestRefresh.UserID)
		if err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}

		_, refreshToken, err = a.CreateRefreshToken(ctx, user.ID)
		if err != nil {
			log.Error("failed to create refresh token", logger.Err(err))
			return fmt.Errorf("%s: %w", op, err)
		}

		log.Info("refresh token created")

		accessToken, err = jwt.NewAccessToken(latestRefresh.UserID, user.Username, a.accessTTL, a.tokenSecret)
		if err != nil {
			log.Error("failed to create access token", logger.Err(err))
			return fmt.Errorf("%s: %w", op, err)
		}

		return nil
	})
	if err != nil {
		return accessToken, refreshToken, fmt.Errorf("%s: %w", op, err)
	}

	return accessToken, refreshToken, nil
}
