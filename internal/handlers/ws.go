package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"voice-chat-api/internal/dto"
	grpcsignaling "voice-chat-api/internal/grpc/signaling"
	mw "voice-chat-api/internal/middlewares"
	"voice-chat-api/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}

		originURL, err := url.Parse(origin)
		if err != nil {
			return false
		}

		return strings.EqualFold(originURL.Host, r.Host)
	},
}

const (
	createSession = "create_session"
	joinSession   = "join_session"
	resumeSession = "resume_session"
	webrtcOffer   = "webrtc_offer"
	webrtcAnswer  = "webrtc_answer"
	iceCandidate  = "ice_candidate"
)

type WSHandlerConfig struct {
	EnqueueTimeout      time.Duration
	LeaveTimeout        time.Duration
	ControlWriteTimeout time.Duration
	PongWait            time.Duration
	PingPeriod          time.Duration
	WriteTimeout        time.Duration
	ReadLimit           int64
	SendBufferSize      int
}

type SignalingService interface {
	CreateSession(ctx context.Context, creator *models.Client) (uuid.UUID, string, error)
	JoinSession(ctx context.Context, sessionID uuid.UUID, client *models.Client) (string, error)
	ResumeSession(ctx context.Context, reconnectToken string, client *models.Client) (uuid.UUID, string, error)
	LeaveSession(ctx context.Context, client *models.Client) error
	SetOffer(ctx context.Context, sessionID uuid.UUID, sdp string, client *models.Client) error
	SetAnswer(ctx context.Context, sessionID uuid.UUID, sdp string, client *models.Client) error
	SetCandidate(
		ctx context.Context,
		sessionID uuid.UUID,
		candidate,
		sdpMID,
		usernameFragment string,
		SDPMLineIndex uint16,
		client *models.Client,
	) error
}

type WSHandler struct {
	service SignalingService
	log     *slog.Logger
	cfg     WSHandlerConfig
}

func NewWSHandler(
	log *slog.Logger,
	service SignalingService,
	cfg WSHandlerConfig,
) *WSHandler {
	return &WSHandler{
		log:     log,
		service: service,
		cfg:     cfg,
	}
}

func (h *WSHandler) Handle(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "cannot upgrade", http.StatusBadRequest)
		return
	}
	defer conn.Close()
	h.configureConn(conn)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	userID, ok := mw.GetUserID(r.Context())
	if !ok {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "missing user context"),
			time.Now().Add(h.cfg.ControlWriteTimeout),
		)
		return
	}

	client := &models.Client{
		UserID:         userID,
		PeerID:         uuid.New(),
		SessionID:      uuid.Nil,
		Conn:           conn,
		Send:           make(chan []byte, h.cfg.SendBufferSize),
		Done:           make(chan struct{}),
		EnqueueTimeout: h.cfg.EnqueueTimeout,
	}
	defer func() {
		close(client.Done)

		leaveCtx, leaveCancel := context.WithTimeout(context.Background(), h.cfg.LeaveTimeout)
		defer leaveCancel()
		if err := h.service.LeaveSession(leaveCtx, client); err != nil {
			h.log.Warn("failed to leave session", "peer_id", client.PeerID, "err", err)
		}
	}()

	go h.writePump(ctx, client)
	h.readPump(ctx, client)
}

func (h *WSHandler) writePump(ctx context.Context, c *models.Client) {
	defer c.Conn.Close()

	pingPeriod := h.cfg.PingPeriod
	if pingPeriod <= 0 {
		pingPeriod = 50 * time.Second
	}
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case msg := <-c.Send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(h.writeTimeout()))
			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				h.log.Warn("failed to write ws message", "peer_id", c.PeerID, "err", err)
				return
			}
		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(h.writeTimeout()))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				h.log.Warn("failed to write ws ping", "peer_id", c.PeerID, "err", err)
				return
			}
		case <-c.Done:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (h *WSHandler) readPump(ctx context.Context, c *models.Client) {
	for {
		_, msg, err := c.Conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(
				err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
				websocket.CloseNoStatusReceived,
			) {
				h.log.Warn("failed to read ws message", "peer_id", c.PeerID, "err", err)
			}
			break
		}

		var m dto.WSMessage
		if err := json.Unmarshal(msg, &m); err != nil {
			if sendErr := h.sendErrorResponse(c, "invalid websocket message"); sendErr != nil {
				h.log.Warn("failed to send ws error response", "peer_id", c.PeerID, "err", sendErr)
				return
			}
			continue
		}

		var handleErr error
		switch m.Type {
		case createSession:
			handleErr = h.handleCreateSession(ctx, c)
		case joinSession:
			handleErr = h.handleJoinSession(ctx, c, m.Data)
		case resumeSession:
			handleErr = h.handleResumeSession(ctx, c, m.Data)
		case webrtcOffer:
			handleErr = h.handleWebRTCOffer(ctx, c, m.Data)
		case webrtcAnswer:
			handleErr = h.handleWebRTCAnswer(ctx, c, m.Data)
		case iceCandidate:
			handleErr = h.handleICECandidate(ctx, c, m.Data)
		default:
			handleErr = fmt.Errorf("unknown message type")
		}

		if handleErr != nil {
			h.log.Warn("failed to handle ws message", "peer_id", c.PeerID, "type", m.Type, "err", handleErr)
			if sendErr := h.sendErrorResponse(c, clientErrorMessage(handleErr)); sendErr != nil {
				h.log.Warn("failed to send ws error response", "peer_id", c.PeerID, "err", sendErr)
				return
			}
		}
	}
}

func (h *WSHandler) handleCreateSession(ctx context.Context, c *models.Client) error {
	const op = "WSHandler.HandleCreateSession"

	sessionID, reconnectToken, err := h.service.CreateSession(ctx, c)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	data := dto.SessionResponse{
		Status:         dto.Success,
		SessionID:      sessionID,
		PeerID:         c.PeerID,
		ReconnectToken: reconnectToken,
	}

	res, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return c.Enqueue(res)
}

func (h *WSHandler) handleJoinSession(ctx context.Context, c *models.Client, rawData json.RawMessage) error {
	const op = "WSHandler.HandleJoinSession"

	var sessionData dto.SessionData
	if err := json.Unmarshal(rawData, &sessionData); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if sessionData.SessionID == uuid.Nil {
		return fmt.Errorf("%s: session_id is required", op)
	}

	reconnectToken, err := h.service.JoinSession(ctx, sessionData.SessionID, c)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if err := h.sendSessionResponse(c, sessionData.SessionID, reconnectToken); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (h *WSHandler) handleResumeSession(ctx context.Context, c *models.Client, rawData json.RawMessage) error {
	const op = "WSHandler.HandleResumeSession"

	var resumeData dto.ResumeData
	if err := json.Unmarshal(rawData, &resumeData); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if strings.TrimSpace(resumeData.ReconnectToken) == "" {
		return fmt.Errorf("%s: reconnect_token is required", op)
	}

	sessionID, reconnectToken, err := h.service.ResumeSession(ctx, resumeData.ReconnectToken, c)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if err := h.sendSessionResponse(c, sessionID, reconnectToken); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (h *WSHandler) handleWebRTCOffer(ctx context.Context, c *models.Client, rawData json.RawMessage) error {
	const op = "WSHandler.HandleWebRTCOffer"

	var offerData dto.OfferData
	if err := json.Unmarshal(rawData, &offerData); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if offerData.SessionID == uuid.Nil {
		return fmt.Errorf("%s: session_id is required", op)
	}
	if offerData.SDP == "" {
		return fmt.Errorf("%s: sdp is required", op)
	}

	if err := h.service.SetOffer(ctx, offerData.SessionID, offerData.SDP, c); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if err := h.sendSuccessResponse(c); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (h *WSHandler) handleWebRTCAnswer(ctx context.Context, c *models.Client, rawData json.RawMessage) error {
	const op = "WSHandler.HandleWebRTCAnswer"

	var answerData dto.AnswerData
	if err := json.Unmarshal(rawData, &answerData); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if answerData.SessionID == uuid.Nil {
		return fmt.Errorf("%s: session_id is required", op)
	}
	if answerData.SDP == "" {
		return fmt.Errorf("%s: sdp is required", op)
	}

	if err := h.service.SetAnswer(ctx, answerData.SessionID, answerData.SDP, c); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if err := h.sendSuccessResponse(c); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (h *WSHandler) handleICECandidate(ctx context.Context, c *models.Client, rawData json.RawMessage) error {
	const op = "WSHandler.HandleICECandidate"

	var candidateData dto.IceCandidateData
	if err := json.Unmarshal(rawData, &candidateData); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if candidateData.SessionID == uuid.Nil {
		return fmt.Errorf("%s: session_id is required", op)
	}
	if candidateData.Candidate == "" {
		return fmt.Errorf("%s: candidate is required", op)
	}

	if err := h.service.SetCandidate(
		ctx,
		candidateData.SessionID,
		candidateData.Candidate,
		candidateData.SDPMid,
		candidateData.UsernameFragment,
		candidateData.SDPMLineIndex,
		c,
	); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if err := h.sendSuccessResponse(c); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (h *WSHandler) sendSuccessResponse(c *models.Client) error {
	const op = "WSHandler.SendSuccessResponse"

	data := dto.WSResponse{
		Status: dto.Success,
	}

	res, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return c.Enqueue(res)
}

func (h *WSHandler) sendSessionResponse(c *models.Client, sessionID uuid.UUID, reconnectToken string) error {
	const op = "WSHandler.SendSessionResponse"

	data := dto.SessionResponse{
		Status:         dto.Success,
		SessionID:      sessionID,
		PeerID:         c.PeerID,
		ReconnectToken: reconnectToken,
	}

	res, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return c.Enqueue(res)
}

func (h *WSHandler) sendErrorResponse(c *models.Client, message string) error {
	const op = "WSHandler.SendErrorResponse"

	data := dto.WSResponse{
		Status: dto.Error,
		Error:  message,
	}

	res, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return c.Enqueue(res)
}

func (h *WSHandler) configureConn(conn *websocket.Conn) {
	if h.cfg.ReadLimit > 0 {
		conn.SetReadLimit(h.cfg.ReadLimit)
	}

	pongWait := h.pongWait()
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
}

func (h *WSHandler) pongWait() time.Duration {
	if h.cfg.PongWait > 0 {
		return h.cfg.PongWait
	}

	return 60 * time.Second
}

func (h *WSHandler) writeTimeout() time.Duration {
	if h.cfg.WriteTimeout > 0 {
		return h.cfg.WriteTimeout
	}

	return 5 * time.Second
}

func clientErrorMessage(err error) string {
	if errors.Is(err, grpcsignaling.ErrRoomNotFound) {
		return "room not found"
	}

	return err.Error()
}
