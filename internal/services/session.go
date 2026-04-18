package service

import (
	"context"
	"fmt"
	"log/slog"
	"voice-chat-api/internal/models"

	"github.com/google/uuid"
)

type SessionService struct {
	log     *slog.Logger
	storage SessionStorage
}

type SessionStorage interface {
	Create(creator *models.Client) (uuid.UUID, error)
	Get(sessionID uuid.UUID) (*models.Session, error)
	Delete(sessionID uuid.UUID) error
}

func NewSessionService(
	log *slog.Logger,
	storage SessionStorage,
) *SessionService {
	return &SessionService{
		log:     log,
		storage: storage,
	}
}

func (s *SessionService) CreateSession(ctx context.Context, creator *models.Client) (uuid.UUID, error) {
	sessionID, err := s.storage.Create(creator)
	if err != nil {
		return uuid.Nil, fmt.Errorf("") //TODO
	}

	return sessionID, nil
}

func (s *SessionService) JoinSession(ctx context.Context, sessionID uuid.UUID, client *models.Client) error {
	session, err := s.storage.Get(sessionID)
	if err != nil {
		return err //TODO
	}

	session.Clients = append(session.Clients, client)

	return nil
}

func (s *SessionService) ThrowOffer(ctx context.Context, sessionID uuid.UUID, sdp string, client *models.Client) error {
	session, err := s.storage.Get(sessionID)
	if err != nil {
		return err //TODO
	}

	for _, c := range session.Clients {
		if c.PeerID != client.PeerID {
			return fmt.Errorf("") //TODO
		}
	}

	//TODO send offer to sound server by grpc
	//TODO
	//TODO
	//TODO

	return nil
}

func (s *SessionService) ThrowCandidate(
	ctx context.Context,
	sessionID uuid.UUID,
	candidate,
	sdpMID,
	usernameFragment string,
	SDPMLineIndex uint16,
	client *models.Client,
) error {
	session, err := s.storage.Get(sessionID)
	if err != nil {
		return err //TODO
	}

	for _, c := range session.Clients {
		if c.PeerID != client.PeerID {
			return fmt.Errorf("") //TODO
		}
	}

	//TODO send candidate to sound server by grpc
	//TODO
	//TODO
	//TODO

	return nil
}
