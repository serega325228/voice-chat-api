package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"voice-chat-api/internal/dto"
	service "voice-chat-api/internal/services"
)

const invalidRequestBodyMessage = "invalid request body"

type Validator interface {
	Struct(s any) error
}

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
	log       *slog.Logger
	service   AuthService
	validator Validator
}

func NewUserHandler(log *slog.Logger, service AuthService, validator Validator) *UserHandler {
	return &UserHandler{log: log, service: service, validator: validator}
}

func (h *UserHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req dto.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, invalidRequestBodyMessage, http.StatusBadRequest)
		h.log.Error("failed to decode user data", "err", err)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)

	if err := h.validator.Struct(req); err != nil {
		http.Error(w, invalidRequestBodyMessage, http.StatusBadRequest)
		h.log.Warn("invalid register request", "err", err)
		return
	}

	access, refresh, err := h.service.RegisterUser(r.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUserAlreadyExists):
			http.Error(w, "user already exists", http.StatusConflict)
		default:
			h.log.Error("failed to register user", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	tokens := dto.TokenResponse{Access: access, Refresh: refresh}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(tokens); err != nil {
		h.log.Error("failed to encode tokens", "err", err)
		return
	}
}

func (h *UserHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, invalidRequestBodyMessage, http.StatusBadRequest)
		h.log.Error("failed to decode user data", "err", err)
		return
	}
	req.Email = strings.TrimSpace(req.Email)

	if err := h.validator.Struct(req); err != nil {
		http.Error(w, invalidRequestBodyMessage, http.StatusBadRequest)
		h.log.Warn("invalid login request", "err", err)
		return
	}

	access, refresh, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
		default:
			h.log.Error("failed to login user", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	tokens := dto.TokenResponse{Access: access, Refresh: refresh}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(tokens); err != nil {
		h.log.Error("failed to encode tokens", "err", err)
		return
	}
}
