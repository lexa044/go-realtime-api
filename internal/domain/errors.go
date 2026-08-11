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

	// ErrUserNotFound indicates no user exists with the given username.
	// Internal only — AuthService.Login translates it to
	// ErrInvalidCredentials before it ever reaches an HTTP response, so a
	// client can't distinguish "unknown username" from "wrong password".
	ErrUserNotFound = errors.New("user not found")

	// ErrInvalidCredentials is the only auth failure ever exposed to a
	// caller of /auth/login — deliberately generic, covering unknown
	// username, wrong password, and disabled account alike.
	ErrInvalidCredentials = errors.New("invalid username or password")

	// ErrRefreshTokenInvalid covers every reason a refresh token can't be
	// used to mint a new access token: unknown, expired, or already
	// revoked (including the reuse-detection case in AuthService.Refresh).
	ErrRefreshTokenInvalid = errors.New("invalid or expired refresh token")
)
