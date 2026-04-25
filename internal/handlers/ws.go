package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"voice-chat-api/internal/dto"
	mw "voice-chat-api/internal/middlewares"
	"voice-chat-api/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

const (
	createSession = "create_session"
	joinSession   = "join_session"
	webrtcOffer   = "webrtc_offer"
	iceCandidate  = "ice_candidate"
)

type SignalingService interface {
	CreateSession(ctx context.Context, creator *models.Client) (uuid.UUID, error)
	JoinSession(ctx context.Context, sessionID uuid.UUID, client *models.Client) error
	SetOffer(ctx context.Context, sessionID uuid.UUID, sdp string, client *models.Client) error
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
}

func NewWSHandler(
	log *slog.Logger,
	service SignalingService,
) *WSHandler {
	return &WSHandler{
		log:     log,
		service: service,
	}
}

func (h *WSHandler) Handle(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "cannot upgrade", http.StatusBadRequest)
		return
	}
	defer conn.Close()

	peerID, err := uuid.NewUUID()
	if err != nil {
		return //TODO
	}

	userID, ok := mw.GetUserID(r.Context())
	if !ok {
		return //TODO
	}

	client := &models.Client{
		UserID: userID,
		PeerID: peerID,
		Conn:   conn,
		Send:   make(chan []byte),
	}

	go h.writePump(r.Context(), client)
	h.readPump(r.Context(), client)
}

func (h *WSHandler) writePump(ctx context.Context, c *models.Client) {
	for {
		select {
		case msg := <-c.Send:
			c.Conn.WriteMessage(websocket.TextMessage, msg)
		case <-ctx.Done():
			return
		}
	}
}

func (h *WSHandler) readPump(ctx context.Context, c *models.Client) {
	for {
		_, msg, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}

		var m dto.WSMessage
		json.Unmarshal(msg, &m)

		switch m.Type {
		case createSession:
			sessionID, err := h.service.CreateSession(ctx, c)
			if err != nil {
				continue //TODO
			}

			data := dto.SessionResponse{
				Status:    dto.Success,
				SessionID: sessionID,
			}

			res, err := json.Marshal(data)
			if err != nil {
				continue //TODO
			}

			c.Send <- res

		case joinSession:

			var sessionData dto.SessionData
			json.Unmarshal(m.Data, &sessionData)

			sessionID := sessionData.SessionID
			if err := h.service.JoinSession(ctx, sessionID, c); err != nil {
				continue
			}

			data := dto.WSResponse{
				Status: dto.Success,
			}

			res, err := json.Marshal(data)
			if err != nil {
				continue //TODO
			}

			c.Send <- res

		case webrtcOffer:
			var offerData dto.OfferData
			json.Unmarshal(m.Data, &offerData)
			err := h.service.SetOffer(ctx, offerData.SessionID, offerData.SDP, c)
			if err != nil {
				continue
			}

			data := dto.WSResponse{
				Status: dto.Success,
			}

			res, err := json.Marshal(data)
			if err != nil {
				continue //TODO
			}

			c.Send <- res

		case iceCandidate:
			var candidateData dto.IceCandidateData
			json.Unmarshal(m.Data, &candidateData)
			if err := h.service.SetCandidate(
				ctx,
				candidateData.SessionID,
				candidateData.Candidate,
				candidateData.SDPMid,
				candidateData.UsernameFragment,
				candidateData.SDPMLineIndex,
				c,
			); err != nil {
				continue //TODO
			}

			data := dto.WSResponse{
				Status: dto.Success,
			}

			res, err := json.Marshal(data)
			if err != nil {
				continue //TODO
			}

			c.Send <- res

		default:
			//TODO
		}
	}
}
