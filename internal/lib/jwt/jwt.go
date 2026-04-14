package jwt

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	Access  = "access"
	Refresh = "refresh"
)

type Claims struct {
	UserID uuid.UUID `json:"sub"`
	jwt.RegisteredClaims
}

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

func VerifyToken(tokenString string, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {

		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}

		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("invalid claims")
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
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
