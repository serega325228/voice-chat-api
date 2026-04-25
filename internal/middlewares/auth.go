package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"voice-chat-api/internal/lib/jwt"

	"github.com/google/uuid"
)

type contextKey string

const userIDKey contextKey = "userID"

var (
	errEmptyAuthorizationHeader = errors.New("empty authorization header")
	errInvalidAuthHeader        = errors.New("invalid auth header")
)

func extractBearer(header string) (string, error) {
	const op = "AuthMiddleware.ExtractBearer"

	if header == "" {
		return "", fmt.Errorf("%s: %w", op, errEmptyAuthorizationHeader)
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", fmt.Errorf("%s: %w", op, errInvalidAuthHeader)
	}

	return strings.TrimPrefix(header, prefix), nil
}

func GetUserID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userIDKey).(uuid.UUID)
	return id, ok
}

func AuthMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			tokenString, err := extractBearer(r.Header.Get("Authorization"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}

			claims, err := jwt.VerifyAccessToken(tokenString, secret)
			if err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
