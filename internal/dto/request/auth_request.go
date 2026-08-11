package request

// LoginRequest is the wire shape for POST /auth/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// RefreshRequest is the wire shape for POST /auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// LogoutRequest is the wire shape for POST /auth/logout.
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}
