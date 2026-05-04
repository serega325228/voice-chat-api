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
		familyID,
		userID uuid.UUID,
		tokenHash [32]byte,
		status models.RefreshTokenStatus,
		created_at,
		expires_at time.Time,
	) (uuid.UUID, error)
	GetByHash(ctx context.Context, tokenHash [32]byte) (*models.RefreshToken, error)
	GetByHashForUpdate(ctx context.Context, tokenHash [32]byte) (*models.RefreshToken, error)
	GetByID(ctx context.Context, tokenID uuid.UUID) (*models.RefreshToken, error)
	GetLatestByUserID(ctx context.Context, userID uuid.UUID) (*models.RefreshToken, error)
	ChangeStatus(ctx context.Context, tokenID uuid.UUID, status models.RefreshTokenStatus) error
	ChangeFamilyStatus(ctx context.Context, familyID uuid.UUID, statusBefore, statusAfter models.RefreshTokenStatus) error
}

type UserProvider interface {
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	GetByID(ctx context.Context, userID uuid.UUID) (*models.User, error)
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
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
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
	const op = "AuthService.Login"

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
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	if err := bcrypt.CompareHashAndPassword(user.PassHash, []byte(password)); err != nil {
		log.Info("invalid credentials", logger.Err(err))
		return "", "", fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
	}

	log.Info("user logged is successfully")

	familyID := uuid.New()

	return a.CreateNewTokens(ctx, familyID, user.ID)
}

func (a *AuthService) RegisterUser(
	ctx context.Context,
	username string,
	email string,
	password string,
) (string, string, error) {
	const op = "AuthService.RegisterUser"

	log := a.log.With(
		slog.String("op", op),
	)

	log.Info("registering new user")

	_, err := a.userProvider.GetByEmail(ctx, email)
	if err == nil {
		log.Error("email already registered")
		return "", "", fmt.Errorf("%s: %w", op, ErrUserAlreadyExists)
	}
	if !errors.Is(err, storageErrors.ErrUserNotFound) {
		log.Error("failed to check existing user", logger.Err(err))
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	passHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Error("failed to generate password hash", logger.Err(err))
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	userID, err := a.userProvider.Create(ctx, username, email, passHash)
	if err != nil {
		if errors.Is(err, storageErrors.ErrUserExists) {
			log.Error("email already registered")
			return "", "", fmt.Errorf("%s: %w", op, ErrUserAlreadyExists)
		}
		log.Error("failed to save user", logger.Err(err))
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	log.Info("new user registered")

	familyID := uuid.New()

	return a.CreateNewTokens(ctx, familyID, userID)
}

func (a *AuthService) CreateNewTokens(ctx context.Context, familyID, userID uuid.UUID) (string, string, error) {
	const op = "AuthService.CreateNewTokens"

	log := a.log.With(
		slog.String("op", op),
	)

	log.Info("creating new pair of tokens")

	refreshToken, err := a.CreateRefreshToken(ctx, familyID, userID)
	if err != nil {
		log.Error("failed to create refresh token", logger.Err(err))
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	log.Info("refresh token created")

	accessToken, err := jwt.NewAccessToken(userID, a.accessTTL, a.tokenSecret)
	if err != nil {
		log.Error("failed to create access token", logger.Err(err))
		return "", "", fmt.Errorf("%s: %w", op, err)
	}

	return accessToken, refreshToken, nil
}

func (a *AuthService) CreateRefreshToken(ctx context.Context, familyID, userID uuid.UUID) (string, error) {
	const op = "AuthService.CreateRefreshToken"

	log := a.log.With(
		slog.String("op", op),
	)

	log.Info("creating new refresh token")

	tokenString, iat, exp, err := jwt.NewRefreshToken(userID, a.refreshTTL, a.tokenSecret)
	if err != nil {
		log.Error("failed to generate refresh token", logger.Err(err))
		return "", fmt.Errorf("%s: %w", op, err)
	}

	tokenHash := sha256.Sum256([]byte(tokenString))
	if _, err = a.tokenProvider.Create(ctx, familyID, userID, tokenHash, models.Active, iat, exp); err != nil {
		log.Error("failed to save token", logger.Err(err))
		return "", fmt.Errorf("%s: %w", op, err)
	}

	log.Info("new refresh token created")

	return tokenString, nil
}

func (a *AuthService) MarkRefreshTokenAsRotated(ctx context.Context, token *models.RefreshToken) error {
	const op = "AuthService.MarkRefreshTokenAsRotated"

	log := a.log.With(
		slog.String("op", op),
	)

	log.Info("mark refresh token as rotated")

	if err := a.tokenProvider.ChangeStatus(ctx, token.ID, models.Rotated); err != nil {
		log.Error("failed to change token status", logger.Err(err))
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (a *AuthService) Refresh(ctx context.Context, refreshString string) (string, string, error) {
	const op = "AuthService.Refresh"

	log := a.log.With(
		slog.String("op", op),
	)

	claims, err := jwt.VerifyRefreshToken(refreshString, a.tokenSecret)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", op, ErrInvalidRefreshToken)
	}
	tokenHash := sha256.Sum256([]byte(refreshString))

	var accessToken, refreshToken string

	err = a.txProvider.WithTx(ctx, func(ctx context.Context) error {
		latestRefresh, err := a.tokenProvider.GetByHashForUpdate(ctx, tokenHash)
		if err != nil {
			if errors.Is(err, storageErrors.ErrTokenNotFound) {
				log.Warn("refresh token not found", logger.Err(err))
				return fmt.Errorf("%s: %w", op, ErrInvalidRefreshToken)
			}
			log.Error("failed find latest token by hash", logger.Err(err))
			return fmt.Errorf("%s: %w", op, err)
		}

		if latestRefresh.UserID != claims.UserID {
			log.Error("token user mismatch")
			return fmt.Errorf("%s: %w", op, ErrInvalidRefreshToken)
		}

		if latestRefresh.Status == models.Rotated {
			log.Error("token already is rotated")
			if err = a.tokenProvider.ChangeFamilyStatus(ctx, latestRefresh.FamilyID, models.Active, models.Rotated); err != nil {
				return fmt.Errorf("%s: %w", op, err)
			}
			return fmt.Errorf("%s: %w", op, ErrTokenAlreadyRotated)
		}

		if latestRefresh.Status != models.Active {
			log.Error("token is not active", "status", latestRefresh.Status)
			return fmt.Errorf("%s: %w", op, ErrInvalidRefreshToken)
		}

		if latestRefresh.ExpiresAt.Before(time.Now()) {
			log.Error("token expired in storage")
			return fmt.Errorf("%s: %w", op, ErrInvalidRefreshToken)
		}

		if err = a.MarkRefreshTokenAsRotated(ctx, latestRefresh); err != nil {
			log.Error("failed mark token as rotated", logger.Err(err))
			return fmt.Errorf("%s: %w", op, err)
		}

		accessToken, refreshToken, err = a.CreateNewTokens(ctx, latestRefresh.FamilyID, latestRefresh.UserID)
		if err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}

		return nil
	})
	if err != nil {
		return accessToken, refreshToken, fmt.Errorf("%s: %w", op, err)
	}

	return accessToken, refreshToken, nil
}
