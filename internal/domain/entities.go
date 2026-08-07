package domain

import "time"

// Order is a core business entity. It has zero knowledge of transport,
// persistence or messaging concerns — that's the whole point of the
// dependency rule in clean architecture.
type Order struct {
	ID         string
	CustomerID string
	Status     string
	Total      float64
	CreatedAt  time.Time
	UpdatedAt  *time.Time // nil until the order is first updated or deleted
	IsDeleted  bool
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
