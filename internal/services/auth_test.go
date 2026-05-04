package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
	storageErrors "voice-chat-api/internal/errors"
	appjwt "voice-chat-api/internal/lib/jwt"
	"voice-chat-api/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const (
	testSecret     = "test-secret"
	testAccessTTL  = 15 * time.Minute
	testRefreshTTL = 24 * time.Hour
)

type MockUserProvider struct {
	mock.Mock
}

func (m *MockUserProvider) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	args := m.Called(ctx, email)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserProvider) GetByID(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	args := m.Called(ctx, userID)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserProvider) Create(
	ctx context.Context,
	username string,
	email string,
	passHash []byte,
) (uuid.UUID, error) {
	args := m.Called(ctx, username, email, passHash)

	if args.Get(0) == nil {
		return uuid.Nil, args.Error(1)
	}

	return args.Get(0).(uuid.UUID), args.Error(1)
}

type MockTokenProvider struct {
	mock.Mock
}

func (m *MockTokenProvider) Create(
	ctx context.Context,
	familyID uuid.UUID,
	userID uuid.UUID,
	tokenHash [32]byte,
	status models.RefreshTokenStatus,
	createdAt time.Time,
	expiresAt time.Time,
) (uuid.UUID, error) {
	args := m.Called(ctx, familyID, userID, tokenHash, status, createdAt, expiresAt)

	if args.Get(0) == nil {
		return uuid.Nil, args.Error(1)
	}

	return args.Get(0).(uuid.UUID), args.Error(1)
}

func (m *MockTokenProvider) GetByHash(ctx context.Context, tokenHash [32]byte) (*models.RefreshToken, error) {
	args := m.Called(ctx, tokenHash)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*models.RefreshToken), args.Error(1)
}

func (m *MockTokenProvider) GetByHashForUpdate(ctx context.Context, tokenHash [32]byte) (*models.RefreshToken, error) {
	args := m.Called(ctx, tokenHash)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*models.RefreshToken), args.Error(1)
}

func (m *MockTokenProvider) GetByID(ctx context.Context, tokenID uuid.UUID) (*models.RefreshToken, error) {
	args := m.Called(ctx, tokenID)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*models.RefreshToken), args.Error(1)
}

func (m *MockTokenProvider) GetLatestByUserID(ctx context.Context, userID uuid.UUID) (*models.RefreshToken, error) {
	args := m.Called(ctx, userID)

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*models.RefreshToken), args.Error(1)
}

func (m *MockTokenProvider) ChangeStatus(ctx context.Context, tokenID uuid.UUID, status models.RefreshTokenStatus) error {
	args := m.Called(ctx, tokenID, status)
	return args.Error(0)
}

func (m *MockTokenProvider) ChangeFamilyStatus(
	ctx context.Context,
	familyID uuid.UUID,
	statusBefore models.RefreshTokenStatus,
	statusAfter models.RefreshTokenStatus,
) error {
	args := m.Called(ctx, familyID, statusBefore, statusAfter)
	return args.Error(0)
}

type MockTxProvider struct {
	calls       int
	receivedCtx context.Context
	withTx      func(ctx context.Context, fn func(ctx context.Context) error) error
}

func (m *MockTxProvider) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	m.calls++
	m.receivedCtx = ctx

	if m.withTx != nil {
		return m.withTx(ctx, fn)
	}

	return fn(ctx)
}

func TestAuthService_Login(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userID := uuid.New()
	passHash := mustHashPassword(t, "password")
	upstreamErr := errors.New("db unavailable")

	tests := []struct {
		name          string
		email         string
		password      string
		mockUser      func(*MockUserProvider)
		mockTokens    func(*MockTokenProvider)
		expectedError error
	}{
		{
			name:     "success",
			email:    "user@example.com",
			password: "password",
			mockUser: func(userRepo *MockUserProvider) {
				userRepo.
					On("GetByEmail", ctx, "user@example.com").
					Return(&models.User{
						ID:       userID,
						Email:    "user@example.com",
						PassHash: passHash,
					}, nil).
					Once()
			},
			mockTokens: func(tokenRepo *MockTokenProvider) {
				tokenRepo.
					On(
						"Create",
						ctx,
						mock.AnythingOfType("uuid.UUID"),
						userID,
						mock.AnythingOfType("[32]uint8"),
						models.Active,
						mock.AnythingOfType("time.Time"),
						mock.AnythingOfType("time.Time"),
					).
					Return(uuid.New(), nil).
					Once()
			},
		},
		{
			name:     "user not found",
			email:    "missing@example.com",
			password: "password",
			mockUser: func(userRepo *MockUserProvider) {
				userRepo.
					On("GetByEmail", ctx, "missing@example.com").
					Return(nil, storageErrors.ErrUserNotFound).
					Once()
			},
			mockTokens:    func(*MockTokenProvider) {},
			expectedError: ErrInvalidCredentials,
		},
		{
			name:     "unexpected get user error",
			email:    "user@example.com",
			password: "password",
			mockUser: func(userRepo *MockUserProvider) {
				userRepo.
					On("GetByEmail", ctx, "user@example.com").
					Return(nil, upstreamErr).
					Once()
			},
			mockTokens:    func(*MockTokenProvider) {},
			expectedError: upstreamErr,
		},
		{
			name:     "invalid password",
			email:    "user@example.com",
			password: "wrong-password",
			mockUser: func(userRepo *MockUserProvider) {
				userRepo.
					On("GetByEmail", ctx, "user@example.com").
					Return(&models.User{
						ID:       userID,
						Email:    "user@example.com",
						PassHash: passHash,
					}, nil).
					Once()
			},
			mockTokens:    func(*MockTokenProvider) {},
			expectedError: ErrInvalidCredentials,
		},
		{
			name:     "token creation error",
			email:    "user@example.com",
			password: "password",
			mockUser: func(userRepo *MockUserProvider) {
				userRepo.
					On("GetByEmail", ctx, "user@example.com").
					Return(&models.User{
						ID:       userID,
						Email:    "user@example.com",
						PassHash: passHash,
					}, nil).
					Once()
			},
			mockTokens: func(tokenRepo *MockTokenProvider) {
				tokenRepo.
					On(
						"Create",
						ctx,
						mock.AnythingOfType("uuid.UUID"),
						userID,
						mock.AnythingOfType("[32]uint8"),
						models.Active,
						mock.AnythingOfType("time.Time"),
						mock.AnythingOfType("time.Time"),
					).
					Return(nil, upstreamErr).
					Once()
			},
			expectedError: upstreamErr,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userRepo := new(MockUserProvider)
			tokenRepo := new(MockTokenProvider)

			tt.mockUser(userRepo)
			tt.mockTokens(tokenRepo)

			authService := newTestAuthService(userRepo, tokenRepo, nil)

			accessToken, refreshToken, err := authService.Login(ctx, tt.email, tt.password)

			assert.ErrorIs(t, err, tt.expectedError)
			if tt.expectedError == nil {
				require.NotEmpty(t, accessToken)
				require.NotEmpty(t, refreshToken)
				assertAccessTokenForUser(t, accessToken, userID)
				assertRefreshTokenForUser(t, refreshToken, userID)
			} else {
				assert.Empty(t, accessToken)
				assert.Empty(t, refreshToken)
				require.Error(t, err)
			}

			userRepo.AssertExpectations(t)
			tokenRepo.AssertExpectations(t)
		})
	}
}

func TestAuthService_RegisterUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userID := uuid.New()
	upstreamErr := errors.New("storage failure")

	tests := []struct {
		name          string
		username      string
		email         string
		password      string
		mockUser      func(*MockUserProvider)
		mockTokens    func(*MockTokenProvider)
		expectedError error
	}{
		{
			name:     "success",
			username: "alice",
			email:    "alice@example.com",
			password: "password123",
			mockUser: func(userRepo *MockUserProvider) {
				userRepo.
					On("GetByEmail", ctx, "alice@example.com").
					Return(nil, storageErrors.ErrUserNotFound).
					Once()
				userRepo.
					On(
						"Create",
						ctx,
						"alice",
						"alice@example.com",
						mock.MatchedBy(func(hash []byte) bool {
							return len(hash) > 0 && bcrypt.CompareHashAndPassword(hash, []byte("password123")) == nil
						}),
					).
					Return(userID, nil).
					Once()
			},
			mockTokens: func(tokenRepo *MockTokenProvider) {
				tokenRepo.
					On(
						"Create",
						ctx,
						mock.AnythingOfType("uuid.UUID"),
						userID,
						mock.AnythingOfType("[32]uint8"),
						models.Active,
						mock.AnythingOfType("time.Time"),
						mock.AnythingOfType("time.Time"),
					).
					Return(uuid.New(), nil).
					Once()
			},
		},
		{
			name:     "email already exists on lookup",
			username: "alice",
			email:    "alice@example.com",
			password: "password123",
			mockUser: func(userRepo *MockUserProvider) {
				userRepo.
					On("GetByEmail", ctx, "alice@example.com").
					Return(&models.User{ID: userID}, nil).
					Once()
			},
			mockTokens:    func(*MockTokenProvider) {},
			expectedError: ErrUserAlreadyExists,
		},
		{
			name:     "unexpected lookup error",
			username: "alice",
			email:    "alice@example.com",
			password: "password123",
			mockUser: func(userRepo *MockUserProvider) {
				userRepo.
					On("GetByEmail", ctx, "alice@example.com").
					Return(nil, upstreamErr).
					Once()
			},
			mockTokens:    func(*MockTokenProvider) {},
			expectedError: upstreamErr,
		},
		{
			name:     "duplicate on create",
			username: "alice",
			email:    "alice@example.com",
			password: "password123",
			mockUser: func(userRepo *MockUserProvider) {
				userRepo.
					On("GetByEmail", ctx, "alice@example.com").
					Return(nil, storageErrors.ErrUserNotFound).
					Once()
				userRepo.
					On(
						"Create",
						ctx,
						"alice",
						"alice@example.com",
						mock.AnythingOfType("[]uint8"),
					).
					Return(nil, storageErrors.ErrUserExists).
					Once()
			},
			mockTokens:    func(*MockTokenProvider) {},
			expectedError: ErrUserAlreadyExists,
		},
		{
			name:     "unexpected create error",
			username: "alice",
			email:    "alice@example.com",
			password: "password123",
			mockUser: func(userRepo *MockUserProvider) {
				userRepo.
					On("GetByEmail", ctx, "alice@example.com").
					Return(nil, storageErrors.ErrUserNotFound).
					Once()
				userRepo.
					On(
						"Create",
						ctx,
						"alice",
						"alice@example.com",
						mock.AnythingOfType("[]uint8"),
					).
					Return(nil, upstreamErr).
					Once()
			},
			mockTokens:    func(*MockTokenProvider) {},
			expectedError: upstreamErr,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userRepo := new(MockUserProvider)
			tokenRepo := new(MockTokenProvider)

			tt.mockUser(userRepo)
			tt.mockTokens(tokenRepo)

			authService := newTestAuthService(userRepo, tokenRepo, nil)

			accessToken, refreshToken, err := authService.RegisterUser(ctx, tt.username, tt.email, tt.password)

			assert.ErrorIs(t, err, tt.expectedError)
			if tt.expectedError == nil {
				require.NotEmpty(t, accessToken)
				require.NotEmpty(t, refreshToken)
				assertAccessTokenForUser(t, accessToken, userID)
				assertRefreshTokenForUser(t, refreshToken, userID)
			} else {
				assert.Empty(t, accessToken)
				assert.Empty(t, refreshToken)
				require.Error(t, err)
				assert.ErrorContains(t, err, "AuthService.RegisterUser")
			}

			userRepo.AssertExpectations(t)
			tokenRepo.AssertExpectations(t)
		})
	}
}

func TestAuthService_CreateRefreshToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	familyID := uuid.New()
	userID := uuid.New()
	upstreamErr := errors.New("insert failed")

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On(
				"Create",
				ctx,
				familyID,
				userID,
				mock.MatchedBy(func(hash [32]byte) bool {
					return hash != [32]byte{}
				}),
				models.Active,
				mock.MatchedBy(func(createdAt time.Time) bool {
					return !createdAt.IsZero()
				}),
				mock.MatchedBy(func(expiresAt time.Time) bool {
					return expiresAt.After(time.Now())
				}),
			).
			Return(uuid.New(), nil).
			Once()

		authService := newTestAuthService(new(MockUserProvider), tokenRepo, nil)

		tokenString, err := authService.CreateRefreshToken(ctx, familyID, userID)

		require.NoError(t, err)
		require.NotEmpty(t, tokenString)
		assertRefreshTokenForUser(t, tokenString, userID)
		tokenRepo.AssertExpectations(t)
	})

	t.Run("token provider error", func(t *testing.T) {
		t.Parallel()

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On(
				"Create",
				ctx,
				familyID,
				userID,
				mock.AnythingOfType("[32]uint8"),
				models.Active,
				mock.AnythingOfType("time.Time"),
				mock.AnythingOfType("time.Time"),
			).
			Return(nil, upstreamErr).
			Once()

		authService := newTestAuthService(new(MockUserProvider), tokenRepo, nil)

		tokenString, err := authService.CreateRefreshToken(ctx, familyID, userID)

		assert.Empty(t, tokenString)
		require.Error(t, err)
		assert.ErrorIs(t, err, upstreamErr)
		assert.ErrorContains(t, err, "AuthService.CreateRefreshToken")
		tokenRepo.AssertExpectations(t)
	})
}

func TestAuthService_CreateNewTokens(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	familyID := uuid.New()
	userID := uuid.New()
	upstreamErr := errors.New("save refresh failed")

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On(
				"Create",
				ctx,
				familyID,
				userID,
				mock.AnythingOfType("[32]uint8"),
				models.Active,
				mock.AnythingOfType("time.Time"),
				mock.AnythingOfType("time.Time"),
			).
			Return(uuid.New(), nil).
			Once()

		authService := newTestAuthService(new(MockUserProvider), tokenRepo, nil)

		accessToken, refreshToken, err := authService.CreateNewTokens(ctx, familyID, userID)

		require.NoError(t, err)
		require.NotEmpty(t, accessToken)
		require.NotEmpty(t, refreshToken)
		assertAccessTokenForUser(t, accessToken, userID)
		assertRefreshTokenForUser(t, refreshToken, userID)
		tokenRepo.AssertExpectations(t)
	})

	t.Run("refresh token creation error", func(t *testing.T) {
		t.Parallel()

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On(
				"Create",
				ctx,
				familyID,
				userID,
				mock.AnythingOfType("[32]uint8"),
				models.Active,
				mock.AnythingOfType("time.Time"),
				mock.AnythingOfType("time.Time"),
			).
			Return(nil, upstreamErr).
			Once()

		authService := newTestAuthService(new(MockUserProvider), tokenRepo, nil)

		accessToken, refreshToken, err := authService.CreateNewTokens(ctx, familyID, userID)

		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		require.Error(t, err)
		assert.ErrorIs(t, err, upstreamErr)
		assert.ErrorContains(t, err, "AuthService.CreateNewTokens")
		tokenRepo.AssertExpectations(t)
	})
}

func TestAuthService_MarkRefreshTokenAsRotated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tokenID := uuid.New()
	upstreamErr := errors.New("update failed")

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On("ChangeStatus", ctx, tokenID, models.Rotated).
			Return(nil).
			Once()

		authService := newTestAuthService(new(MockUserProvider), tokenRepo, nil)

		err := authService.MarkRefreshTokenAsRotated(ctx, &models.RefreshToken{ID: tokenID})

		require.NoError(t, err)
		tokenRepo.AssertExpectations(t)
	})

	t.Run("change status error", func(t *testing.T) {
		t.Parallel()

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On("ChangeStatus", ctx, tokenID, models.Rotated).
			Return(upstreamErr).
			Once()

		authService := newTestAuthService(new(MockUserProvider), tokenRepo, nil)

		err := authService.MarkRefreshTokenAsRotated(ctx, &models.RefreshToken{ID: tokenID})

		require.Error(t, err)
		assert.ErrorIs(t, err, upstreamErr)
		assert.ErrorContains(t, err, "AuthService.MarkRefreshTokenAsRotated")
		tokenRepo.AssertExpectations(t)
	})
}

func TestAuthService_Refresh(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userID := uuid.New()
	otherUserID := uuid.New()
	familyID := uuid.New()
	tokenID := uuid.New()
	upstreamErr := errors.New("token storage error")

	t.Run("invalid token string", func(t *testing.T) {
		t.Parallel()

		txProvider := &MockTxProvider{}
		authService := newTestAuthService(new(MockUserProvider), new(MockTokenProvider), txProvider)

		accessToken, refreshToken, err := authService.Refresh(ctx, "not-a-jwt")

		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidRefreshToken)
		assert.Equal(t, 0, txProvider.calls)
	})

	t.Run("token not found", func(t *testing.T) {
		t.Parallel()

		refreshString, _, _, err := appjwt.NewRefreshToken(userID, testRefreshTTL, testSecret)
		require.NoError(t, err)
		expectedHash := sha256.Sum256([]byte(refreshString))

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On("GetByHashForUpdate", ctx, expectedHash).
			Return(nil, storageErrors.ErrTokenNotFound).
			Once()

		txProvider := &MockTxProvider{}
		authService := newTestAuthService(new(MockUserProvider), tokenRepo, txProvider)

		accessToken, refreshToken, err := authService.Refresh(ctx, refreshString)

		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidRefreshToken)
		assert.Equal(t, 1, txProvider.calls)
		tokenRepo.AssertExpectations(t)
	})

	t.Run("unexpected get token error", func(t *testing.T) {
		t.Parallel()

		refreshString, _, _, err := appjwt.NewRefreshToken(userID, testRefreshTTL, testSecret)
		require.NoError(t, err)
		expectedHash := sha256.Sum256([]byte(refreshString))

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On("GetByHashForUpdate", ctx, expectedHash).
			Return(nil, upstreamErr).
			Once()

		txProvider := &MockTxProvider{}
		authService := newTestAuthService(new(MockUserProvider), tokenRepo, txProvider)

		accessToken, refreshToken, refreshErr := authService.Refresh(ctx, refreshString)

		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		require.Error(t, refreshErr)
		assert.ErrorIs(t, refreshErr, upstreamErr)
		assert.ErrorContains(t, refreshErr, "AuthService.Refresh")
		tokenRepo.AssertExpectations(t)
	})

	t.Run("user mismatch", func(t *testing.T) {
		t.Parallel()

		refreshString, _, _, err := appjwt.NewRefreshToken(userID, testRefreshTTL, testSecret)
		require.NoError(t, err)
		expectedHash := sha256.Sum256([]byte(refreshString))

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On("GetByHashForUpdate", ctx, expectedHash).
			Return(&models.RefreshToken{
				ID:        tokenID,
				UserID:    otherUserID,
				FamilyID:  familyID,
				Status:    models.Active,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil).
			Once()

		txProvider := &MockTxProvider{}
		authService := newTestAuthService(new(MockUserProvider), tokenRepo, txProvider)

		accessToken, refreshToken, refreshErr := authService.Refresh(ctx, refreshString)

		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		require.Error(t, refreshErr)
		assert.ErrorIs(t, refreshErr, ErrInvalidRefreshToken)
		tokenRepo.AssertExpectations(t)
	})

	t.Run("rotated token rotates family and returns domain error", func(t *testing.T) {
		t.Parallel()

		refreshString, _, _, err := appjwt.NewRefreshToken(userID, testRefreshTTL, testSecret)
		require.NoError(t, err)
		expectedHash := sha256.Sum256([]byte(refreshString))

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On("GetByHashForUpdate", ctx, expectedHash).
			Return(&models.RefreshToken{
				ID:        tokenID,
				UserID:    userID,
				FamilyID:  familyID,
				Status:    models.Rotated,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil).
			Once()
		tokenRepo.
			On("ChangeFamilyStatus", ctx, familyID, models.Active, models.Rotated).
			Return(nil).
			Once()

		txProvider := &MockTxProvider{}
		authService := newTestAuthService(new(MockUserProvider), tokenRepo, txProvider)

		accessToken, refreshToken, refreshErr := authService.Refresh(ctx, refreshString)

		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		require.Error(t, refreshErr)
		assert.ErrorIs(t, refreshErr, ErrTokenAlreadyRotated)
		tokenRepo.AssertExpectations(t)
	})

	t.Run("change family status error", func(t *testing.T) {
		t.Parallel()

		refreshString, _, _, err := appjwt.NewRefreshToken(userID, testRefreshTTL, testSecret)
		require.NoError(t, err)
		expectedHash := sha256.Sum256([]byte(refreshString))

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On("GetByHashForUpdate", ctx, expectedHash).
			Return(&models.RefreshToken{
				ID:        tokenID,
				UserID:    userID,
				FamilyID:  familyID,
				Status:    models.Rotated,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil).
			Once()
		tokenRepo.
			On("ChangeFamilyStatus", ctx, familyID, models.Active, models.Rotated).
			Return(upstreamErr).
			Once()

		txProvider := &MockTxProvider{}
		authService := newTestAuthService(new(MockUserProvider), tokenRepo, txProvider)

		accessToken, refreshToken, refreshErr := authService.Refresh(ctx, refreshString)

		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		require.Error(t, refreshErr)
		assert.ErrorIs(t, refreshErr, upstreamErr)
		assert.ErrorContains(t, refreshErr, "AuthService.Refresh")
		tokenRepo.AssertExpectations(t)
	})

	t.Run("non active token", func(t *testing.T) {
		t.Parallel()

		refreshString, _, _, err := appjwt.NewRefreshToken(userID, testRefreshTTL, testSecret)
		require.NoError(t, err)
		expectedHash := sha256.Sum256([]byte(refreshString))

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On("GetByHashForUpdate", ctx, expectedHash).
			Return(&models.RefreshToken{
				ID:        tokenID,
				UserID:    userID,
				FamilyID:  familyID,
				Status:    models.Revoked,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil).
			Once()

		txProvider := &MockTxProvider{}
		authService := newTestAuthService(new(MockUserProvider), tokenRepo, txProvider)

		accessToken, refreshToken, refreshErr := authService.Refresh(ctx, refreshString)

		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		require.Error(t, refreshErr)
		assert.ErrorIs(t, refreshErr, ErrInvalidRefreshToken)
		tokenRepo.AssertExpectations(t)
	})

	t.Run("expired token in storage", func(t *testing.T) {
		t.Parallel()

		refreshString, _, _, err := appjwt.NewRefreshToken(userID, testRefreshTTL, testSecret)
		require.NoError(t, err)
		expectedHash := sha256.Sum256([]byte(refreshString))

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On("GetByHashForUpdate", ctx, expectedHash).
			Return(&models.RefreshToken{
				ID:        tokenID,
				UserID:    userID,
				FamilyID:  familyID,
				Status:    models.Active,
				ExpiresAt: time.Now().Add(-time.Minute),
			}, nil).
			Once()

		txProvider := &MockTxProvider{}
		authService := newTestAuthService(new(MockUserProvider), tokenRepo, txProvider)

		accessToken, refreshToken, refreshErr := authService.Refresh(ctx, refreshString)

		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		require.Error(t, refreshErr)
		assert.ErrorIs(t, refreshErr, ErrInvalidRefreshToken)
		tokenRepo.AssertExpectations(t)
	})

	t.Run("change status error", func(t *testing.T) {
		t.Parallel()

		refreshString, _, _, err := appjwt.NewRefreshToken(userID, testRefreshTTL, testSecret)
		require.NoError(t, err)
		expectedHash := sha256.Sum256([]byte(refreshString))

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On("GetByHashForUpdate", ctx, expectedHash).
			Return(&models.RefreshToken{
				ID:        tokenID,
				UserID:    userID,
				FamilyID:  familyID,
				Status:    models.Active,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil).
			Once()
		tokenRepo.
			On("ChangeStatus", ctx, tokenID, models.Rotated).
			Return(upstreamErr).
			Once()

		txProvider := &MockTxProvider{}
		authService := newTestAuthService(new(MockUserProvider), tokenRepo, txProvider)

		accessToken, refreshToken, refreshErr := authService.Refresh(ctx, refreshString)

		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		require.Error(t, refreshErr)
		assert.ErrorIs(t, refreshErr, upstreamErr)
		assert.ErrorContains(t, refreshErr, "AuthService.Refresh")
		tokenRepo.AssertExpectations(t)
	})

	t.Run("create new tokens error after rotation", func(t *testing.T) {
		t.Parallel()

		refreshString, _, _, err := appjwt.NewRefreshToken(userID, testRefreshTTL, testSecret)
		require.NoError(t, err)
		expectedHash := sha256.Sum256([]byte(refreshString))

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On("GetByHashForUpdate", ctx, expectedHash).
			Return(&models.RefreshToken{
				ID:        tokenID,
				UserID:    userID,
				FamilyID:  familyID,
				Status:    models.Active,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil).
			Once()
		tokenRepo.
			On("ChangeStatus", ctx, tokenID, models.Rotated).
			Return(nil).
			Once()
		tokenRepo.
			On(
				"Create",
				ctx,
				familyID,
				userID,
				mock.AnythingOfType("[32]uint8"),
				models.Active,
				mock.AnythingOfType("time.Time"),
				mock.AnythingOfType("time.Time"),
			).
			Return(nil, upstreamErr).
			Once()

		txProvider := &MockTxProvider{}
		authService := newTestAuthService(new(MockUserProvider), tokenRepo, txProvider)

		accessToken, refreshToken, refreshErr := authService.Refresh(ctx, refreshString)

		assert.Empty(t, accessToken)
		assert.Empty(t, refreshToken)
		require.Error(t, refreshErr)
		assert.ErrorIs(t, refreshErr, upstreamErr)
		assert.ErrorContains(t, refreshErr, "AuthService.Refresh")
		tokenRepo.AssertExpectations(t)
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		refreshString, _, _, err := appjwt.NewRefreshToken(userID, testRefreshTTL, testSecret)
		require.NoError(t, err)
		expectedHash := sha256.Sum256([]byte(refreshString))

		tokenRepo := new(MockTokenProvider)
		tokenRepo.
			On("GetByHashForUpdate", ctx, expectedHash).
			Return(&models.RefreshToken{
				ID:        tokenID,
				UserID:    userID,
				FamilyID:  familyID,
				Status:    models.Active,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil).
			Once()
		tokenRepo.
			On("ChangeStatus", ctx, tokenID, models.Rotated).
			Return(nil).
			Once()
		tokenRepo.
			On(
				"Create",
				ctx,
				familyID,
				userID,
				mock.AnythingOfType("[32]uint8"),
				models.Active,
				mock.AnythingOfType("time.Time"),
				mock.AnythingOfType("time.Time"),
			).
			Return(uuid.New(), nil).
			Once()

		txProvider := &MockTxProvider{}
		authService := newTestAuthService(new(MockUserProvider), tokenRepo, txProvider)

		accessToken, nextRefreshToken, refreshErr := authService.Refresh(ctx, refreshString)

		require.NoError(t, refreshErr)
		require.NotEmpty(t, accessToken)
		require.NotEmpty(t, nextRefreshToken)
		assertAccessTokenForUser(t, accessToken, userID)
		assertRefreshTokenForUser(t, nextRefreshToken, userID)
		assert.Equal(t, 1, txProvider.calls)
		assert.Equal(t, ctx, txProvider.receivedCtx)
		tokenRepo.AssertExpectations(t)
	})
}

func newTestAuthService(userRepo UserProvider, tokenRepo TokenProvider, txProvider TxProvider) *AuthService {
	return NewAuthService(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		userRepo,
		tokenRepo,
		txProvider,
		testAccessTTL,
		testRefreshTTL,
		testSecret,
	)
}

func mustHashPassword(t *testing.T, password string) []byte {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)

	return hash
}

func assertAccessTokenForUser(t *testing.T, token string, userID uuid.UUID) {
	t.Helper()

	claims, err := appjwt.VerifyAccessToken(token, testSecret)
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, appjwt.Access, claims.Type)
}

func assertRefreshTokenForUser(t *testing.T, token string, userID uuid.UUID) {
	t.Helper()

	claims, err := appjwt.VerifyRefreshToken(token, testSecret)
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, appjwt.Refresh, claims.Type)
}
