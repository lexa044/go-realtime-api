package request

// PlaceOrderRequest is the wire shape for POST /orders.
type PlaceOrderRequest struct {
	CustomerID string  `json:"customer_id"`
	Total      float64 `json:"total"`
	Currency   string  `json:"currency,omitempty"` // defaults to USD if omitted
}

// UpdateOrderRequest is the wire shape for PUT /orders/{id}. It's a full
// replace, not a partial patch — every field is required.
type UpdateOrderRequest struct {
	CustomerID string  `json:"customer_id"`
	Status     string  `json:"status"`
	Total      float64 `json:"total"`
	Currency   string  `json:"currency,omitempty"` // defaults to USD if omitted
}
