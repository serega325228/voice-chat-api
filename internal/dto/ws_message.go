package dto

import (
	"encoding/json"

	"github.com/google/uuid"
)

type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type SessionData struct {
	SessionID uuid.UUID `json:"session_id"`
}

type OfferData struct {
	SessionID uuid.UUID `json:"session_id"`
	SDP       string    `json:"sdp"`
}

type IceCandidateData struct {
	SessionID        uuid.UUID `json:"session_id"`
	Candidate        string    `json:"candidate"`
	SDPMid           string    `json:"sdp_mid"`
	SDPMLineIndex    uint16    `json:"sdp_mline_index"`
	UsernameFragment string    `json:"username_fragment,omitempty"`
}
