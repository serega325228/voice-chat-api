package models

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	sessionv1 "github.com/serega325228/voice-chat-sfu-protos/gen/go/session"
	"google.golang.org/grpc"
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

	mu           sync.Mutex
	signalStream grpc.BidiStreamingClient[sessionv1.SignalMessage, sessionv1.SignalMessage]
	signalCancel context.CancelFunc
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

func (c *Client) GetSignalStream() (grpc.BidiStreamingClient[sessionv1.SignalMessage, sessionv1.SignalMessage], bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.signalStream == nil {
		return nil, false
	}

	return c.signalStream, true
}

func (c *Client) SetSignalStream(
	stream grpc.BidiStreamingClient[sessionv1.SignalMessage, sessionv1.SignalMessage],
	cancel context.CancelFunc,
) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.signalStream = stream
	c.signalCancel = cancel
}

func (c *Client) TakeSignalStream() (
	grpc.BidiStreamingClient[sessionv1.SignalMessage, sessionv1.SignalMessage],
	context.CancelFunc,
) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stream := c.signalStream
	cancel := c.signalCancel

	c.signalStream = nil
	c.signalCancel = nil

	return stream, cancel
}
