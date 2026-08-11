package response

import "time"

// OrderResponse is the wire shape returned for a single order. Having a
// dedicated response type — rather than serializing domain.Order directly
// — means internal representation choices (a Money value object, a
// *time.Time for "never updated") don't leak into the API contract
// unmodified.
type OrderResponse struct {
	ID         string     `json:"id"`
	CustomerID string     `json:"customer_id"`
	Status     string     `json:"status"`
	Total      float64    `json:"total"`
	Currency   string     `json:"currency"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
	IsDeleted  bool       `json:"is_deleted"`
	CreatedBy  string     `json:"created_by"` // user ID of whoever created the order
	UpdatedBy  string     `json:"updated_by"` // user ID of whoever last created/updated/deleted it
}

// ListOrdersResponse is the wire shape for GET /orders.
type ListOrdersResponse struct {
	Data       []OrderResponse `json:"data"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	TotalCount int             `json:"total_count"`
}
