package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/lexa044/realtime-api/internal/adapter/security"
	"github.com/lexa044/realtime-api/internal/contextutil"
)

var errMissingToken = errors.New("missing token")

// AuthMiddleware validates an access token from either the Authorization
// header (normal REST calls) or a `token` query parameter (the websocket
// handshake — browsers can't set custom headers when opening a WS
// connection, so the token has to travel some other way). Verification
// itself lives in adapter/security, shared with the code that issues these
// tokens in the first place, so signing and verifying can never drift out
// of sync. On success, the user ID is stashed under contextutil.UserIDKey
// for downstream handlers.
func AuthMiddleware(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenString, err := extractToken(r)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			userID, err := security.VerifyAccessToken(secret, tokenString)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), contextutil.UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractToken(r *http.Request) (string, error) {
	if auth := r.Header.Get("Authorization"); auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return parts[1], nil
		}
	}
	if tok := r.URL.Query().Get("token"); tok != "" {
		return tok, nil
	}
	return "", errMissingToken
}
