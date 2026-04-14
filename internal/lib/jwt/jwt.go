package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	Access  = "access"
	Refresh = "refresh"
)

func NewAccessToken(userID uuid.UUID, duration time.Duration, secret string) (string, error) {
	token := jwt.New(jwt.SigningMethodHS256)

	claims := token.Claims.(jwt.MapClaims)
	claims["sub"] = userID
	claims["exp"] = time.Now().Add(duration).Unix()
	claims["iat"] = time.Now().Unix()
	claims["typ"] = Access

	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func NewRefreshToken(userID uuid.UUID, duration time.Duration, secret string) (string, time.Time, time.Time, error) {
	token := jwt.New(jwt.SigningMethodHS256)

	exp := time.Now().Add(duration)
	iat := time.Now()

	claims := token.Claims.(jwt.MapClaims)
	claims["sub"] = userID
	claims["exp"] = exp
	claims["iat"] = iat
	claims["typ"] = Refresh

	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", iat, exp, err
	}

	return tokenString, iat, exp, nil
}
