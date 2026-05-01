package signaling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
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

type Service struct {
	log    *slog.Logger
	client sessionv1.SessionClient

	mu    sync.Mutex
	peers map[uuid.UUID]*models.Client
}

func New(log *slog.Logger, conn *grpc.ClientConn) *Service {
	return &Service{
		log:    log,
		client: sessionv1.NewSessionClient(conn),
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
	if err := s.ensureSignalStream(ctx, creator); err != nil {
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
	if err := s.ensureSignalStream(ctx, client); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Service) LeaveSession(ctx context.Context, client *models.Client) error {
	const op = "SignalingService.LeaveSession"

	s.unregisterPeer(client.PeerID)

	_, cancel := client.TakeSignalStream()
	if cancel != nil {
		cancel()
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

func (s *Service) SetOffer(ctx context.Context, sessionID uuid.UUID, sdp string, client *models.Client) error {
	const op = "SignalingService.SetOffer"

	if err := s.bindSession(ctx, sessionID, client); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	_, err := s.client.SendSignal(ctx, &sessionv1.SendSignalRequest{
		Message: &sessionv1.SignalMessage{
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
		},
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Service) SetAnswer(ctx context.Context, sessionID uuid.UUID, sdp string, client *models.Client) error {
	const op = "SignalingService.SetAnswer"

	if err := s.bindSession(ctx, sessionID, client); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	_, err := s.client.SendSignal(ctx, &sessionv1.SendSignalRequest{
		Message: &sessionv1.SignalMessage{
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
		},
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Service) SetCandidate(
	ctx context.Context,
	sessionID uuid.UUID,
	candidate,
	sdpMID,
	usernameFragment string,
	sdpMLineIndex uint16,
	client *models.Client,
) error {
	const op = "SignalingService.SetCandidate"

	if err := s.bindSession(ctx, sessionID, client); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	_, err := s.client.SendSignal(ctx, &sessionv1.SendSignalRequest{
		Message: &sessionv1.SignalMessage{
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
		},
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Service) bindSession(
	ctx context.Context,
	sessionID uuid.UUID,
	client *models.Client,
) error {
	const op = "SignalingService.BindSession"

	if client.SessionID == uuid.Nil {
		client.SessionID = sessionID
	}

	if client.SessionID != sessionID {
		return fmt.Errorf("%s: %w", op, fmt.Errorf("peer %s already bound to session %s", client.PeerID, client.SessionID))
	}

	if err := s.ensureSignalStream(ctx, client); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Service) ensureSignalStream(ctx context.Context, client *models.Client) error {
	const op = "SignalingService.EnsureSignalStream"

	s.registerPeer(client)

	if _, ok := client.GetSignalStream(); ok {
		return nil
	}

	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := s.client.OpenSignalStream(streamCtx, &sessionv1.OpenSignalStreamRequest{
		SessionId: client.SessionID.String(),
		PeerId:    client.PeerID.String(),
	})
	if err != nil {
		cancel()
		return fmt.Errorf("%s: %w", op, err)
	}

	if _, ok := client.GetSignalStream(); ok {
		cancel()
		return nil
	}

	client.SetSignalStream(stream, cancel)

	go s.readLoop(client, stream)

	return nil
}

func (s *Service) readLoop(
	client *models.Client,
	stream grpc.ServerStreamingClient[sessionv1.SignalMessage],
) {
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err != io.EOF && !errors.Is(err, context.Canceled) {
				s.log.Warn("signal stream closed", "peer_id", client.PeerID, "session_id", client.SessionID, "err", err)
			}
			takenStream, cancel := client.TakeSignalStream()
			s.unregisterPeer(client.PeerID)
			if cancel != nil {
				cancel()
			}
			_ = takenStream
			return
		}

		if err := s.forwardToWebSocket(msg); err != nil {
			s.log.Warn("failed to forward signaling message", "peer_id", client.PeerID, "session_id", client.SessionID, "err", err)
		}
	}
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
