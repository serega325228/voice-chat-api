package models

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	sessionv1 "github.com/serega325228/voice-chat-sfu-protos/gen/go/session"
	"google.golang.org/grpc"
)

var (
	ErrClientClosed        = errors.New("client is closed")
	ErrClientBackpressured = errors.New("client is backpressured")
)

type Client struct {
	Conn           *websocket.Conn
	UserID         uuid.UUID
	PeerID         uuid.UUID
	SessionID      uuid.UUID
	Send           chan []byte
	Done           chan struct{}
	EnqueueTimeout time.Duration

	mu           sync.Mutex
	streamInitMu sync.Mutex
	signalStream grpc.BidiStreamingClient[sessionv1.SignalMessage, sessionv1.SignalMessage]
	signalCancel context.CancelFunc
	reconnecting bool
}

func (c *Client) Enqueue(msg []byte) error {
	timer := time.NewTimer(c.EnqueueTimeout)
	defer timer.Stop()

	select {
	case <-c.Done:
		return ErrClientClosed
	case c.Send <- msg:
		return nil
	case <-timer.C:
		return ErrClientBackpressured
	}
}

func (c *Client) LockSignalStreamInit() {
	c.streamInitMu.Lock()
}

func (c *Client) UnlockSignalStreamInit() {
	c.streamInitMu.Unlock()
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

func (c *Client) StartReconnect() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.reconnecting {
		return false
	}

	c.reconnecting = true
	return true
}

func (c *Client) FinishReconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.reconnecting = false
}
