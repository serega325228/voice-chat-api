package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"voice-chat-api/internal/dto"
)

type TokenHandler struct {
	log     *slog.Logger
	service AuthService
}

func NewTokenHandler(log *slog.Logger, service AuthService) *TokenHandler {
	return &TokenHandler{log: log, service: service}
}

func (h *TokenHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	const op = "user.Refresh"
	var oldRefresh dto.RefreshRequest
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewDecoder(r.Body).Decode(&oldRefresh); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		h.log.Error("failed to decode token data")
		return
	}

	accessToken, refreshToken, err := h.service.Refresh(r.Context(), oldRefresh.Refresh)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	tokens := dto.TokenResponse{Access: accessToken, Refresh: refreshToken}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(tokens); err != nil {
		h.log.Error("failed to encode tokens")
		return
	}
}
