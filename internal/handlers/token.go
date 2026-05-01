package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"voice-chat-api/internal/dto"
	service "voice-chat-api/internal/services"
)

type TokenHandler struct {
	log       *slog.Logger
	service   AuthService
	validator Validator
}

func NewTokenHandler(log *slog.Logger, service AuthService, validator Validator) *TokenHandler {
	return &TokenHandler{log: log, service: service, validator: validator}
}

func (h *TokenHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var oldRefresh dto.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&oldRefresh); err != nil {
		http.Error(w, invalidRequestBodyMessage, http.StatusBadRequest)
		h.log.Error("failed to decode token data", "err", err)
		return
	}
	oldRefresh.Refresh = strings.TrimSpace(oldRefresh.Refresh)

	if err := h.validator.Struct(oldRefresh); err != nil {
		http.Error(w, invalidRequestBodyMessage, http.StatusBadRequest)
		h.log.Warn("invalid refresh request", "err", err)
		return
	}

	accessToken, refreshToken, err := h.service.Refresh(r.Context(), oldRefresh.Refresh)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidRefreshToken), errors.Is(err, service.ErrTokenAlreadyRotated):
			http.Error(w, "invalid refresh token", http.StatusUnauthorized)
		default:
			h.log.Error("failed to refresh token", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	tokens := dto.TokenResponse{Access: accessToken, Refresh: refreshToken}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(tokens); err != nil {
		h.log.Error("failed to encode tokens", "err", err)
		return
	}
}
