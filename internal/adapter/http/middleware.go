package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/lexa044/realtime-api/internal/adapter/ws"
)

type contextKey string

const ctxKeyClaims contextKey = "claims"

var errMissingToken = errors.New("missing token")

// Claims is the JWT payload this API expects. Adjust to match your issuer
// (an IdP, a separate auth service, etc). "sub" carries the user id.
type Claims struct {
	jwt.RegisteredClaims
}

// AuthMiddleware validates a JWT from either the Authorization header
// (normal REST calls) or a `token` query parameter (the websocket
// handshake — browsers can't set custom headers when opening a WS
// connection, so the token has to travel some other way). On success it
// stashes the user id under ws.CtxKeyUserID for downstream handlers.
func AuthMiddleware(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenString, err := extractToken(r)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			claims := &Claims{}
			token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing method")
				}
				return secret, nil
			})
			if err != nil || !token.Valid || claims.Subject == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), ws.CtxKeyUserID, claims.Subject)
			ctx = context.WithValue(ctx, ctxKeyClaims, claims)
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
