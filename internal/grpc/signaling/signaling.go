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

type peerStream struct {
	stream grpc.BidiStreamingClient[sessionv1.SignalMessage, sessionv1.SignalMessage]
	cancel context.CancelFunc
}

type Service struct {
	log    *slog.Logger
	client sessionv1.SessionClient

	mu      sync.Mutex
	streams map[uuid.UUID]*peerStream
}

func New(log *slog.Logger, conn *grpc.ClientConn) *Service {
	return &Service{
		log:     log,
		client:  sessionv1.NewSessionClient(conn),
		streams: make(map[uuid.UUID]*peerStream),
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

	stream := s.takePeerStream(client.PeerID)
	if stream != nil {
		stream.cancel()
		_ = stream.stream.CloseSend()
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

	stream, err := s.getOrCreateStream(ctx, sessionID, client)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	err = stream.Send(&sessionv1.SignalMessage{
		SessionId: sessionID.String(),
		PeerId:    client.PeerID.String(),
		Payload: &sessionv1.SignalMessage_RemoteOffer{
			RemoteOffer: &sessionv1.RemoteOffer{
				Offer: &sessionv1.SessionDescription{
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

	stream, err := s.getOrCreateStream(ctx, sessionID, client)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	err = stream.Send(&sessionv1.SignalMessage{
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

func (s *Service) getOrCreateStream(
	ctx context.Context,
	sessionID uuid.UUID,
	client *models.Client,
) (grpc.BidiStreamingClient[sessionv1.SignalMessage, sessionv1.SignalMessage], error) {
	const op = "SignalingService.GetOrCreateStream"

	if client.SessionID == uuid.Nil {
		client.SessionID = sessionID
	}

	if client.SessionID != sessionID {
		return nil, fmt.Errorf("%s: %w", op, fmt.Errorf("peer %s already bound to session %s", client.PeerID, client.SessionID))
	}

	if err := s.ensureSignalStream(ctx, client); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.streams[client.PeerID].stream, nil
}

func (s *Service) ensureSignalStream(ctx context.Context, client *models.Client) error {
	const op = "SignalingService.EnsureSignalStream"

	s.mu.Lock()
	if _, ok := s.streams[client.PeerID]; ok {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := s.client.SignalPeer(streamCtx)
	if err != nil {
		cancel()
		return fmt.Errorf("%s: %w", op, err)
	}

	// if err := stream.Send(&sessionv1.SignalMessage{
	// 	SessionId: client.SessionID.String(),
	// 	PeerId:    client.PeerID.String(),
	// 	Payload: &sessionv1.SignalMessage_ConnectionStateChanged{
	// 		ConnectionStateChanged: &sessionv1.PeerConnectionStateChanged{
	// 			State: initialConnectionStateName,
	// 		},
	// 	},
	// }); err != nil {
	// 	cancel()
	// 	return fmt.Errorf("%s: %w", op, err)
	// }

	s.mu.Lock()
	if _, ok := s.streams[client.PeerID]; ok {
		s.mu.Unlock()
		cancel()
		_ = stream.CloseSend()
		return nil
	}
	s.streams[client.PeerID] = &peerStream{
		stream: stream,
		cancel: cancel,
	}
	s.mu.Unlock()

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
			if peerStream := s.takePeerStream(client.PeerID); peerStream != nil {
				peerStream.cancel()
				_ = peerStream.stream.CloseSend()
			}
			return
		}

		if err := s.forwardToWebSocket(client, msg); err != nil {
			s.log.Warn("failed to forward signaling message", "peer_id", client.PeerID, "session_id", client.SessionID, "err", err)
		}
	}
}

func (s *Service) forwardToWebSocket(client *models.Client, msg *sessionv1.SignalMessage) error {
	const op = "SignalingService.ForwardToWebSocket"

	switch payload := msg.GetPayload().(type) {
	case *sessionv1.SignalMessage_LocalAnswer:
		if err := s.sendWSMessage(client, wsTypeWebRTCAnswer, dto.AnswerData{
			SessionID: client.SessionID,
			SDP:       payload.LocalAnswer.GetAnswer().GetSdp(),
		}); err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
		return nil
	case *sessionv1.SignalMessage_LocalIceCandidate:
		candidate := payload.LocalIceCandidate.GetCandidate()
		if err := s.sendWSMessage(client, wsTypeICECandidate, dto.IceCandidateData{
			SessionID:        client.SessionID,
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
			SessionID: client.SessionID,
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

func (s *Service) takePeerStream(peerID uuid.UUID) *peerStream {
	s.mu.Lock()
	defer s.mu.Unlock()

	stream := s.streams[peerID]
	delete(s.streams, peerID)

	return stream
}
