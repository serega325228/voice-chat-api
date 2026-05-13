package dto

import "github.com/google/uuid"

type WSResponseStatus string

const (
	Success WSResponseStatus = "success"
	Error   WSResponseStatus = "error"
)

type SessionResponse struct {
	Status         WSResponseStatus `json:"status"`
	SessionID      uuid.UUID        `json:"session_id"`
	PeerID         uuid.UUID        `json:"peer_id"`
	ReconnectToken string           `json:"reconnect_token,omitempty"`
}
