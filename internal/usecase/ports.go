package usecase

import (
	"context"
	"time"

	"github.com/lexa044/realtime-api/internal/domain"
)

// --- Driven ports: implemented by adapters, called BY usecases ---

type OrderRepository interface {
	// Create persists o as given; o.CreatedBy/UpdatedBy must already be
	// set by the caller (OrderService) to the acting user's ID.
	Create(ctx context.Context, o *domain.Order) error
	GetByID(ctx context.Context, id string) (*domain.Order, error)

	// List returns a page of non-deleted orders (newest first), optionally
	// filtered by customerID (empty = no filter), plus the total row count
	// matching the filter so callers can compute total pages.
	List(ctx context.Context, customerID string, page, pageSize int) (orders []domain.Order, total int, err error)

	// Update returns domain.ErrOrderNotFound if the order doesn't exist or
	// is already soft-deleted. updatedBy is the acting user's ID, recorded
	// on the row.
	Update(ctx context.Context, id, customerID string, status domain.OrderStatus, total domain.Money, updatedBy string, updatedAt time.Time) (*domain.Order, error)

	// Delete performs a logical delete and returns domain.ErrOrderNotFound
	// if the order doesn't exist or was already deleted. deletedBy is the
	// acting user's ID, recorded as the row's UpdatedBy.
	Delete(ctx context.Context, id, deletedBy string, deletedAt time.Time) error
}

// EventPublisher decouples usecases from the concrete message broker
// (Redis today, Kafka or NATS tomorrow — usecase code never changes).
type EventPublisher interface {
	Publish(ctx context.Context, channel string, event domain.Event) error
}

// UserRepository looks up user accounts. Provisioning (create/update
// password) is deliberately NOT part of this port — there is no self-serve
// registration usecase; accounts are seeded directly against the database
// (see cmd/seeduser and repository.UserRepository.Upsert).
type UserRepository interface {
	GetByUsername(ctx context.Context, username string) (*domain.User, error)
	GetByID(ctx context.Context, id string) (*domain.User, error)
}

// RefreshTokenRepository persists the refresh-token rotation chain.
type RefreshTokenRepository interface {
	Create(ctx context.Context, t *domain.RefreshToken) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error)

	// Revoke marks a token used, optionally linking it to the token that
	// replaced it (nil for a plain logout / no rotation).
	Revoke(ctx context.Context, id string, replacedBy *string, revokedAt time.Time) error

	// RevokeAllForUser revokes every still-valid token for a user — used
	// when AuthService.Refresh detects a revoked token being reused,
	// which signals the token may have been stolen.
	RevokeAllForUser(ctx context.Context, userID string, revokedAt time.Time) error
}

// PasswordHasher hashes and verifies passwords. Implemented with bcrypt in
// adapter/security, but kept behind a port so AuthService never imports a
// hashing library directly.
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Compare returns nil if password matches hash, a non-nil error
	// otherwise. Never distinguish "hash malformed" from "password wrong"
	// in the returned error — AuthService treats any error the same way.
	Compare(hash, password string) error
}

// TokenIssuer creates and hashes auth tokens. Implemented with
// golang-jwt for access tokens and crypto/rand + SHA-256 for refresh
// tokens in adapter/security.
type TokenIssuer interface {
	// IssueAccessToken creates a signed access token for userID, valid
	// for the issuer's configured TTL.
	IssueAccessToken(userID string) (token string, expiresAt time.Time, err error)

	// NewRefreshToken generates a fresh random refresh token. raw is
	// returned to the client; hash is what gets persisted — the raw
	// value is never stored, so a stolen DB dump can't be replayed as a
	// live token.
	NewRefreshToken() (raw string, hash string, err error)

	// HashRefreshToken hashes a client-presented raw refresh token the
	// same way NewRefreshToken does, so it can be looked up by hash.
	HashRefreshToken(raw string) string
}

// --- Driving ports: implemented by usecases, called BY handlers ---

type ListOrdersParams struct {
	CustomerID string // optional filter, empty = all customers
	Page       int    // 1-based; values < 1 are clamped to 1
	PageSize   int    // clamped to [1, 200], defaults to 20 if 0
}

type ListOrdersResult struct {
	Orders     []domain.Order
	Page       int
	PageSize   int
	TotalCount int
}

// OrderService's write methods all take actorUserID first: the ID of the
// authenticated caller, read from the JWT by the HTTP layer and threaded
// through so CreatedBy/UpdatedBy are always attributable to a real user.
type OrderService interface {
	PlaceOrder(ctx context.Context, actorUserID, customerID string, total domain.Money) (*domain.Order, error)
	GetOrder(ctx context.Context, id string) (*domain.Order, error)
	ListOrders(ctx context.Context, params ListOrdersParams) (*ListOrdersResult, error)
	UpdateOrder(ctx context.Context, actorUserID, id, customerID string, status domain.OrderStatus, total domain.Money) (*domain.Order, error)
	DeleteOrder(ctx context.Context, actorUserID, id string) error
}

// TokenPair is what a successful login/refresh returns to the caller.
type TokenPair struct {
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
}

type AuthService interface {
	Login(ctx context.Context, username, password string) (*TokenPair, error)
	Refresh(ctx context.Context, refreshToken string) (*TokenPair, error)
	// Logout is idempotent by design: it never reports whether the
	// presented token was valid, unknown, or already revoked — callers
	// always see success.
	Logout(ctx context.Context, refreshToken string) error
}

// UserService serves reads of a user's own profile. Deliberately
// separate from AuthService — that owns token lifecycle (login/refresh/
// logout), this owns profile data — even though both currently sit on
// top of the same UserRepository.
type UserService interface {
	GetProfile(ctx context.Context, userID string) (*domain.User, error)
}
