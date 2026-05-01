package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	Access  = "access"
	Refresh = "refresh"
)

var (
	errUnexpectedSigningMethod = errors.New("unexpected signing method")
	errInvalidClaims           = errors.New("invalid claims")
	errInvalidToken            = errors.New("invalid token")
	ErrInvalidTokenType        = errors.New("invalid token type")
)

type Claims struct {
	UserID uuid.UUID `json:"sub"`
	Type   string    `json:"typ"`
	jwt.RegisteredClaims
}

func NewAccessToken(userID uuid.UUID, duration time.Duration, secret string) (string, error) {
	const op = "JWT.NewAccessToken"

	now := time.Now()
	claims := Claims{
		UserID: userID,
		Type:   Access,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(duration)),
			ID:        uuid.NewString(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}

	return tokenString, nil
}

func VerifyAccessToken(tokenString string, secret string) (*Claims, error) {
	return verifyToken(tokenString, secret, Access)
}

func VerifyRefreshToken(tokenString string, secret string) (*Claims, error) {
	return verifyToken(tokenString, secret, Refresh)
}

func verifyToken(tokenString string, secret string, expectedType string) (*Claims, error) {
	const op = "JWT.VerifyToken"

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {

		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("%s: %w", op, fmt.Errorf("%w: %s", errUnexpectedSigningMethod, token.Method.Alg()))
		}

		return []byte(secret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("%s: %w", op, errInvalidClaims)
	}

	if !token.Valid {
		return nil, fmt.Errorf("%s: %w", op, errInvalidToken)
	}

	if claims.Type != expectedType {
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidTokenType)
	}

	return claims, nil
}

func NewRefreshToken(userID uuid.UUID, duration time.Duration, secret string) (string, time.Time, time.Time, error) {
	const op = "JWT.NewRefreshToken"

	now := time.Now()
	exp := now.Add(duration)
	claims := Claims{
		UserID: userID,
		Type:   Refresh,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.NewString(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", now, exp, fmt.Errorf("%s: %w", op, err)
	}

	return tokenString, now, exp, nil
}
