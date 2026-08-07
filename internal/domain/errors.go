package domain

import "errors"

// ErrOrderNotFound indicates the requested order doesn't exist, or exists
// but has been logically deleted — API callers see both cases the same way
// (404), while the database keeps the soft-deleted row for audit/history.
var ErrOrderNotFound = errors.New("order not found")
