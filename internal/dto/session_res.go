package dto

import "github.com/google/uuid"

type WSResponseStatus string

const (
	Success WSResponseStatus = "success"
	Error   WSResponseStatus = "error"
)

type SessionResponse struct {
	Status    WSResponseStatus `json:"status"`
	SessionID uuid.UUID        `json:"session_id"`
}
