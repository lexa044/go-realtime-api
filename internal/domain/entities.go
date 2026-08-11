package domain

import "time"

// This file holds every entity in the domain — currently just Order and
// Event. Both have zero knowledge of transport, persistence, or messaging
// concerns; that's the whole point of the dependency rule in clean
// architecture.

// User is a person who can authenticate against the API. Provisioned
// directly against the database (see cmd/seeduser) — there is no
// self-serve registration endpoint by design.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	IsActive     bool
	CreatedAt    time.Time
}

// RefreshToken is one link in a rotation chain: each successful refresh
// revokes the presented token and issues a new one, linked via
// ReplacedBy, so token reuse (a revoked token presented again) is
// detectable — see AuthService.Refresh.
type RefreshToken struct {
	ID         string
	UserID     string
	TokenHash  string // SHA-256 hex digest; the raw token is never persisted
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	ReplacedBy *string
	CreatedAt  time.Time
}

// Order is the core business entity.
type Order struct {
	ID         string
	CustomerID string
	Status     OrderStatus
	Total      Money
	CreatedAt  time.Time
	UpdatedAt  *time.Time // nil until the order is first updated or deleted
	IsDeleted  bool
	CreatedBy  string // ID of the User who created the order
	UpdatedBy  string // ID of the User who last created/updated/deleted it
}

// Event is the payload broadcast to interested parties (websocket clients,
// other services) whenever something relevant happens in the domain.
type Event struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Payload    any       `json:"payload"`
	OccurredAt time.Time `json:"occurred_at"`
}

const (
	EventOrderCreated = "order.created"
	EventOrderUpdated = "order.updated"
	EventOrderDeleted = "order.deleted"
)
