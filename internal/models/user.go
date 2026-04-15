package models

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID        uuid.UUID
	Username  string
	Email     string
	PassHash  []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}
