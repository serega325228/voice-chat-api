package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"voice-chat-api/internal/dto"
	service "voice-chat-api/internal/services"
)

type TokenHandler struct {
	log     *slog.Logger
	service AuthService
}

func NewTokenHandler(log *slog.Logger, service AuthService) *TokenHandler {
	return &TokenHandler{log: log, service: service}
}

func (h *TokenHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var oldRefresh dto.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&oldRefresh); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		h.log.Error("failed to decode token data", "err", err)
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
