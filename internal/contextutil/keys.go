package contextutil

// key is an unexported type so context values set with these keys can't
// collide with keys defined by any other package, per the standard
// library's context.WithValue guidance.
type key int

const (
	// UserIDKey holds the authenticated user's ID. Set by auth middleware
	// on every request; read by both REST handlers and the websocket
	// upgrade handler.
	UserIDKey key = iota

	// ClaimsKey holds the full parsed JWT claims, for handlers that need
	// more than just the user ID.
	ClaimsKey
)
