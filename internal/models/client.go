package models

import (
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Client struct {
	Conn   *websocket.Conn
	UserID uuid.UUID
	PeerID uuid.UUID
	Send   chan []byte
}
