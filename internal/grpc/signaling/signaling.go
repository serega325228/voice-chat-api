package signaling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
	"voice-chat-api/internal/dto"
	"voice-chat-api/internal/models"

	"github.com/google/uuid"
	sessionv1 "github.com/serega325228/voice-chat-sfu-protos/gen/go/session"
	"google.golang.org/grpc"
)

const (
	wsTypeWebRTCAnswer    = "webrtc_answer"
	wsTypeICECandidate    = "ice_candidate"
	wsRenegotiationNeeded = "renegotiation_needed"
)

type Config struct {
	ReconnectMinDelay time.Duration
	ReconnectMaxDelay time.Duration
}

type Service struct {
	log    *slog.Logger
	client sessionv1.SessionClient
	cfg    Config

	mu    sync.Mutex
	peers map[uuid.UUID]*models.Client
}

func New(log *slog.Logger, conn *grpc.ClientConn, cfg Config) *Service {
	return &Service{
		log:    log,
		client: sessionv1.NewSessionClient(conn),
		cfg:    cfg,
		peers:  make(map[uuid.UUID]*models.Client),
	}
}

func (s *Service) CreateSession(ctx context.Context, creator *models.Client) (uuid.UUID, error) {
	const op = "SignalingService.CreateSession"

	sessionID := uuid.New()

	_, err := s.client.CreateSession(ctx, &sessionv1.CreateSessionRequest{
		SessionId: sessionID.String(),
		PeerId:    creator.PeerID.String(),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	creator.SessionID = sessionID
	if err := s.ensureSignalStream(creator); err != nil {
		return uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	return sessionID, nil
}

func (s *Service) JoinSession(ctx context.Context, sessionID uuid.UUID, client *models.Client) error {
	const op = "SignalingService.JoinSession"

	_, err := s.client.JoinSession(ctx, &sessionv1.JoinSessionRequest{
		SessionId: sessionID.String(),
		PeerId:    client.PeerID.String(),
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	client.SessionID = sessionID
	if err := s.ensureSignalStream(client); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Service) LeaveSession(ctx context.Context, client *models.Client) error {
	const op = "SignalingService.LeaveSession"

	s.unregisterPeer(client.PeerID)

	stream, cancel := client.TakeSignalStream()
	if cancel != nil {
		cancel()
	}
	if stream != nil {
		_ = stream.CloseSend()
	}

	if client.SessionID == uuid.Nil {
		return nil
	}

	_, err := s.client.LeaveSession(ctx, &sessionv1.LeaveSessionRequest{
		SessionId: client.SessionID.String(),
		PeerId:    client.PeerID.String(),
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	client.SessionID = uuid.Nil
	return nil
}

func (s *Service) SetOffer(_ context.Context, sessionID uuid.UUID, sdp string, client *models.Client) error {
	const op = "SignalingService.SetOffer"

	err := s.sendSignalMessage(sessionID, client, &sessionv1.SignalMessage{
		SessionId: sessionID.String(),
		PeerId:    client.PeerID.String(),
		Payload: &sessionv1.SignalMessage_RemoteDescription{
			RemoteDescription: &sessionv1.RemoteDescription{
				Description: &sessionv1.SessionDescription{
					Type: sessionv1.SdpType_SDP_TYPE_OFFER,
					Sdp:  sdp,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Service) SetAnswer(_ context.Context, sessionID uuid.UUID, sdp string, client *models.Client) error {
	const op = "SignalingService.SetAnswer"

	err := s.sendSignalMessage(sessionID, client, &sessionv1.SignalMessage{
		SessionId: sessionID.String(),
		PeerId:    client.PeerID.String(),
		Payload: &sessionv1.SignalMessage_RemoteDescription{
			RemoteDescription: &sessionv1.RemoteDescription{
				Description: &sessionv1.SessionDescription{
					Type: sessionv1.SdpType_SDP_TYPE_ANSWER,
					Sdp:  sdp,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Service) SetCandidate(
	_ context.Context,
	sessionID uuid.UUID,
	candidate,
	sdpMID,
	usernameFragment string,
	sdpMLineIndex uint16,
	client *models.Client,
) error {
	const op = "SignalingService.SetCandidate"

	err := s.sendSignalMessage(sessionID, client, &sessionv1.SignalMessage{
		SessionId: sessionID.String(),
		PeerId:    client.PeerID.String(),
		Payload: &sessionv1.SignalMessage_RemoteIceCandidate{
			RemoteIceCandidate: &sessionv1.RemoteIceCandidate{
				Candidate: &sessionv1.IceCandidate{
					Candidate:        candidate,
					SdpMid:           sdpMID,
					SdpMlineIndex:    int32(sdpMLineIndex),
					UsernameFragment: usernameFragment,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Service) sendSignalMessage(
	sessionID uuid.UUID,
	client *models.Client,
	msg *sessionv1.SignalMessage,
) error {
	const op = "SignalingService.sendSignalMessage"

	stream, err := s.getOrCreateStream(sessionID, client)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if err := stream.Send(msg); err == nil {
		return nil
	}

	s.log.Warn("signal send failed, recreating stream", "peer_id", client.PeerID, "session_id", sessionID, "err", err)
	s.resetSpecificSignalStream(client, stream)

	stream, err = s.getOrCreateStream(sessionID, client)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if err := stream.Send(msg); err != nil {
		s.resetSpecificSignalStream(client, stream)
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Service) getOrCreateStream(sessionID uuid.UUID, client *models.Client) (grpc.BidiStreamingClient[sessionv1.SignalMessage, sessionv1.SignalMessage], error) {
	const op = "SignalingService.GetOrCreateStream"

	if client.SessionID == uuid.Nil {
		client.SessionID = sessionID
	}

	if client.SessionID != sessionID {
		return nil, fmt.Errorf("%s: %w", op, fmt.Errorf("peer %s already bound to session %s", client.PeerID, client.SessionID))
	}

	if err := s.ensureSignalStream(client); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	stream, ok := client.GetSignalStream()
	if !ok {
		return nil, fmt.Errorf("%s: signal stream is not initialized", op)
	}

	return stream, nil
}

func (s *Service) ensureSignalStream(client *models.Client) error {
	const op = "SignalingService.EnsureSignalStream"

	s.registerPeer(client)
	client.LockSignalStreamInit()
	defer client.UnlockSignalStreamInit()

	if _, ok := client.GetSignalStream(); ok {
		return nil
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	stream, err := s.client.SignalPeer(streamCtx)
	if err != nil {
		cancel()
		return fmt.Errorf("%s: %w", op, err)
	}

	if err := s.sendAttachHandshake(stream, client); err != nil {
		cancel()
		_ = stream.CloseSend()
		return fmt.Errorf("%s: %w", op, err)
	}

	if _, ok := client.GetSignalStream(); ok {
		cancel()
		_ = stream.CloseSend()
		return nil
	}

	client.SetSignalStream(stream, cancel)

	go s.readLoop(client, stream)

	return nil
}

func (s *Service) readLoop(
	client *models.Client,
	stream grpc.BidiStreamingClient[sessionv1.SignalMessage, sessionv1.SignalMessage],
) {
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err != io.EOF && !errors.Is(err, context.Canceled) {
				s.log.Warn("signal stream closed", "peer_id", client.PeerID, "session_id", client.SessionID, "err", err)
			}
			s.resetSpecificSignalStream(client, stream)
			s.scheduleReconnect(client)
			return
		}

		if err := s.forwardToWebSocket(msg); err != nil {
			s.log.Warn("failed to forward signaling message", "peer_id", client.PeerID, "session_id", client.SessionID, "err", err)
			if errors.Is(err, models.ErrClientBackpressured) {
				_ = client.Conn.Close()
				return
			}
		}
	}
}

func (s *Service) sendAttachHandshake(stream grpc.BidiStreamingClient[sessionv1.SignalMessage, sessionv1.SignalMessage], client *models.Client) error {
	const op = "SignalingService.sendAttachHandshake"

	if client.SessionID == uuid.Nil {
		return fmt.Errorf("%s: peer %s is not bound to session", op, client.PeerID)
	}

	if err := stream.Send(&sessionv1.SignalMessage{
		SessionId: client.SessionID.String(),
		PeerId:    client.PeerID.String(),
	}); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Service) scheduleReconnect(client *models.Client) {
	if client.SessionID == uuid.Nil {
		return
	}
	if !client.StartReconnect() {
		return
	}

	go s.reconnectLoop(client)
}

func (s *Service) reconnectLoop(client *models.Client) {
	defer client.FinishReconnect()

	delay := s.cfg.ReconnectMinDelay

	for {
		if client.SessionID == uuid.Nil {
			return
		}

		select {
		case <-client.Done:
			return
		case <-time.After(delay):
		}

		err := s.ensureSignalStream(client)
		if err == nil {
			s.log.Info("signal stream reattached", "peer_id", client.PeerID, "session_id", client.SessionID)
			return
		}

		s.log.Warn("failed to reattach signal stream", "peer_id", client.PeerID, "session_id", client.SessionID, "err", err)
		delay *= 2
		if delay > s.cfg.ReconnectMaxDelay {
			delay = s.cfg.ReconnectMaxDelay
		}
	}
}

func (s *Service) resetSignalStream(client *models.Client) {
	stream, cancel := client.TakeSignalStream()
	if cancel != nil {
		cancel()
	}
	if stream != nil {
		_ = stream.CloseSend()
	}
}

func (s *Service) resetSpecificSignalStream(
	client *models.Client,
	stream grpc.BidiStreamingClient[sessionv1.SignalMessage, sessionv1.SignalMessage],
) {
	currentStream, ok := client.GetSignalStream()
	if !ok || currentStream != stream {
		return
	}

	s.resetSignalStream(client)
}

func (s *Service) forwardToWebSocket(msg *sessionv1.SignalMessage) error {
	const op = "SignalingService.ForwardToWebSocket"

	peerID, err := uuid.Parse(msg.GetPeerId())
	if err != nil {
		return fmt.Errorf("%s: parse peer_id: %w", op, err)
	}

	client, ok := s.getPeerClient(peerID)
	if !ok {
		return fmt.Errorf("%s: peer %s is not registered", op, peerID)
	}

	sessionID, err := s.sessionIDFromMessage(msg, client)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	switch payload := msg.GetPayload().(type) {
	case *sessionv1.SignalMessage_LocalDescription:
		description := payload.LocalDescription.GetDescription()
		if description.GetType() != sessionv1.SdpType_SDP_TYPE_ANSWER {
			return nil
		}
		if err := s.sendWSMessage(client, wsTypeWebRTCAnswer, dto.AnswerData{
			SessionID: sessionID,
			SDP:       description.GetSdp(),
		}); err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
		return nil
	case *sessionv1.SignalMessage_LocalIceCandidate:
		candidate := payload.LocalIceCandidate.GetCandidate()
		if err := s.sendWSMessage(client, wsTypeICECandidate, dto.IceCandidateData{
			SessionID:        sessionID,
			Candidate:        candidate.GetCandidate(),
			SDPMid:           candidate.GetSdpMid(),
			SDPMLineIndex:    uint16(candidate.GetSdpMlineIndex()),
			UsernameFragment: candidate.GetUsernameFragment(),
		}); err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
		return nil
	case *sessionv1.SignalMessage_RenegotiationNeeded:
		if err := s.sendWSMessage(client, wsRenegotiationNeeded, dto.RenegotiationNeededData{
			SessionID: sessionID,
			SDP:       payload.RenegotiationNeeded.GetOffer().GetSdp(),
		}); err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
		return nil
	default:
		return nil
	}
}

func (s *Service) sendWSMessage(client *models.Client, msgType string, payload any) error {
	const op = "SignalingService.SendWSMessage"

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	rawMessage, err := json.Marshal(dto.WSMessage{
		Type: msgType,
		Data: rawPayload,
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return client.Enqueue(rawMessage)
}

func (s *Service) registerPeer(client *models.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.peers[client.PeerID] = client
}

func (s *Service) getPeerClient(peerID uuid.UUID) (*models.Client, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, ok := s.peers[peerID]
	return client, ok
}

func (s *Service) unregisterPeer(peerID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.peers, peerID)
}

func (s *Service) sessionIDFromMessage(msg *sessionv1.SignalMessage, client *models.Client) (uuid.UUID, error) {
	if rawSessionID := msg.GetSessionId(); rawSessionID != "" {
		sessionID, err := uuid.Parse(rawSessionID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("parse session_id: %w", err)
		}
		return sessionID, nil
	}

	if client.SessionID == uuid.Nil {
		return uuid.Nil, errors.New("signal message missing session_id")
	}

	return client.SessionID, nil
}
