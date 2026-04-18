package sessionstorage

import (
	"fmt"
	"sync"
	"time"
	"voice-chat-api/internal/models"

	"github.com/google/uuid"
)

type SessionStorage struct {
	data map[uuid.UUID]*models.Session
	m    *sync.Mutex
}

func NewSessionStorage() *SessionStorage {
	return &SessionStorage{
		data: make(map[uuid.UUID]*models.Session),
		m:    &sync.Mutex{},
	}
}

func (s *SessionStorage) Create(creator *models.Client) (uuid.UUID, error) {
	id, err := uuid.NewUUID()
	if err != nil {
		return uuid.Nil, err
	}
	clients := []*models.Client{creator}
	s.data[id] = &models.Session{
		Clients:   clients,
		Creator:   creator,
		CreatedAt: time.Now(),
	}

	return id, nil
}

func (s *SessionStorage) Get(sessionID uuid.UUID) (*models.Session, error) {
	s.m.Lock()
	defer s.m.Unlock()
	session, ok := s.data[sessionID]
	if !ok {
		return nil, fmt.Errorf("session with this id doesn't exists")
	}
	return session, nil
}

func (s *SessionStorage) Delete(sessionID uuid.UUID) error {
	s.m.Lock()
	defer s.m.Unlock()
	_, ok := s.data[sessionID]
	if !ok {
		return fmt.Errorf("session with this id doesn't exists")
	}
	delete(s.data, sessionID)
	return nil
}
