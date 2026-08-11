package response

import "time"

// UserResponse is the wire shape for GET /users/me. PasswordHash is
// deliberately never included here or anywhere else — no endpoint in
// this API exposes it.
type UserResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}
