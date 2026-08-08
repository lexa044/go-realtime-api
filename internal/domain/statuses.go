package domain

import "fmt"

// This file holds every enum-like type in the domain — currently just
// OrderStatus, but any future closed-set type (PaymentStatus, ShippingMethod,
// ...) belongs here too, rather than getting its own file.

// OrderStatus is a closed set of valid order states. Using a distinct type
// instead of a bare string stops an arbitrary value like "asdf" from ever
// reaching persistence — the only way to get an OrderStatus is through
// ParseOrderStatus or one of the predefined constants below.
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusShipped   OrderStatus = "shipped"
	OrderStatusCancelled OrderStatus = "cancelled"
)

// ParseOrderStatus validates a raw string against the known set of order
// statuses. Use this at every boundary where an OrderStatus is constructed
// from outside the domain (HTTP request bodies, DB scans).
func ParseOrderStatus(s string) (OrderStatus, error) {
	switch OrderStatus(s) {
	case OrderStatusPending, OrderStatusShipped, OrderStatusCancelled:
		return OrderStatus(s), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidOrderStatus, s)
	}
}

func (s OrderStatus) String() string {
	return string(s)
}
