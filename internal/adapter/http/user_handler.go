package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/lexa044/realtime-api/internal/domain"
	"github.com/lexa044/realtime-api/internal/dto/response"
	"github.com/lexa044/realtime-api/internal/usecase"
)

type UserHandler struct {
	svc usecase.UserService
}

func NewUserHandler(svc usecase.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// Me returns the profile of the authenticated caller. The user ID comes
// from the verified JWT (actorFromContext, same helper OrderHandler uses)
// — never from the URL or a request parameter — so a caller can only ever
// fetch their own profile; there's no way to ask for someone else's.
func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID := actorFromContext(r)

	user, err := h.svc.GetProfile(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			// The JWT was valid but the user it names no longer exists —
			// e.g. deleted after the token was issued but before it expired.
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "could not get user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response.UserResponse{
		ID:        user.ID,
		Username:  user.Username,
		IsActive:  user.IsActive,
		CreatedAt: user.CreatedAt,
	})
}
