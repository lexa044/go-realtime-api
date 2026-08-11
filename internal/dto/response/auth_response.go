package response

import "time"

// TokenPairResponse is returned by both POST /auth/login and
// POST /auth/refresh — a refresh always returns a full new pair (the
// presented refresh token is rotated, not reused).
type TokenPairResponse struct {
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}
