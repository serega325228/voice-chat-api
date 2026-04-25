package models

import (
	"errors"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var (
	ErrClientClosed         = errors.New("client is closed")
	ErrClientSendBufferFull = errors.New("client send buffer is full")
)

type Client struct {
	Conn      *websocket.Conn
	UserID    uuid.UUID
	PeerID    uuid.UUID
	SessionID uuid.UUID
	Send      chan []byte
	Done      chan struct{}
}

func (c *Client) Enqueue(msg []byte) error {
	select {
	case <-c.Done:
		return ErrClientClosed
	case c.Send <- msg:
		return nil
	default:
		return ErrClientSendBufferFull
	}
}
