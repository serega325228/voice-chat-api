package models

import (
	"time"

	"github.com/google/uuid"
)

type RefreshToken struct {
	ID        uuid.UUID
	TokenHash [32]byte
	Status    RefreshTokenStatus
	UserID    uuid.UUID
	FamilyID  uuid.UUID
	ExpiresAt time.Time
	CreatedAt time.Time
}

type RefreshTokenStatus string

const (
	Active  RefreshTokenStatus = "active"
	Rotated RefreshTokenStatus = "rotated"
	Revoked RefreshTokenStatus = "revoked"
)
