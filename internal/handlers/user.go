package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"voice-chat-api/internal/dto"
)

type AuthService interface {
	Login(
		ctx context.Context,
		email string,
		password string,
	) (string, string, error)
	RegisterUser(
		ctx context.Context,
		username string,
		email string,
		password string,
	) (string, string, error)
	Refresh(ctx context.Context, refreshString string) (string, string, error)
}

type UserHandler struct {
	log     *slog.Logger
	service AuthService
}

func NewUserHandler(log *slog.Logger, service AuthService) *UserHandler {
	return &UserHandler{log: log, service: service}
}

func (h *UserHandler) Register(w http.ResponseWriter, r *http.Request) {
	const op = "user.Register"
	var req dto.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		h.log.Error("failed to decode user data")
		return
	}

	access, refresh, err := h.service.RegisterUser(r.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	tokens := dto.TokenResponse{Access: access, Refresh: refresh}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(tokens); err != nil {
		h.log.Error("failed to encode tokens")
		return
	}
}

func (h *UserHandler) Login(w http.ResponseWriter, r *http.Request) {
	const op = "user.Login"
	var req dto.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		h.log.Error("failed to decode user data")
		return
	}

	access, refresh, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	tokens := dto.TokenResponse{Access: access, Refresh: refresh}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(tokens); err != nil {
		h.log.Error("failed to encode tokens")
		return
	}
}
