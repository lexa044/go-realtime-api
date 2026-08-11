package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/lexa044/realtime-api/internal/domain"
	"github.com/lexa044/realtime-api/internal/dto/request"
	"github.com/lexa044/realtime-api/internal/dto/response"
	"github.com/lexa044/realtime-api/internal/usecase"
)

type AuthHandler struct {
	svc usecase.AuthService
}

func NewAuthHandler(svc usecase.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req request.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	pair, err := h.svc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) {
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
			return
		}
		http.Error(w, "could not log in", http.StatusInternalServerError)
		return
	}

	writeTokenPair(w, pair)
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req request.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	pair, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, domain.ErrRefreshTokenInvalid) {
			http.Error(w, "invalid or expired refresh token", http.StatusUnauthorized)
			return
		}
		http.Error(w, "could not refresh token", http.StatusInternalServerError)
		return
	}

	writeTokenPair(w, pair)
}

// Logout always responds 204, whether the presented token was valid,
// unknown, or already revoked — the caller never learns which case it
// was, matching AuthService.Logout's contract.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var req request.LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	_ = h.svc.Logout(r.Context(), req.RefreshToken)
	w.WriteHeader(http.StatusNoContent)
}

func writeTokenPair(w http.ResponseWriter, pair *usecase.TokenPair) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response.TokenPairResponse{
		AccessToken:           pair.AccessToken,
		AccessTokenExpiresAt:  pair.AccessTokenExpiresAt,
		RefreshToken:          pair.RefreshToken,
		RefreshTokenExpiresAt: pair.RefreshTokenExpiresAt,
	})
}
