package domain

import "errors"

// Sentinel errors for the domain package. Centralized here (rather than
// declared next to the type that raises them) so every domain-level
// failure mode is visible from one file.
var (
	// ErrOrderNotFound indicates the requested order doesn't exist, or
	// exists but has been logically deleted — API callers see both cases
	// the same way (404), while the database keeps the soft-deleted row
	// for audit/history.
	ErrOrderNotFound = errors.New("order not found")

	// ErrInvalidOrderStatus indicates a string doesn't match any known
	// OrderStatus constant.
	ErrInvalidOrderStatus = errors.New("invalid order status")

	// ErrInvalidMoney indicates an amount/currency pair failed validation
	// in NewMoney.
	ErrInvalidMoney = errors.New("invalid money amount")
)
