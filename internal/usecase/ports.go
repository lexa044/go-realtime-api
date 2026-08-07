package usecase

import (
	"context"
	"time"

	"github.com/lexa044/realtime-api/internal/domain"
)

// --- Driven ports: implemented by adapters, called BY usecases ---

type OrderRepository interface {
	Create(ctx context.Context, o *domain.Order) error
	GetByID(ctx context.Context, id string) (*domain.Order, error)

	// List returns a page of non-deleted orders (newest first), optionally
	// filtered by customerID (empty = no filter), plus the total row count
	// matching the filter so callers can compute total pages.
	List(ctx context.Context, customerID string, page, pageSize int) (orders []domain.Order, total int, err error)

	// Update returns domain.ErrOrderNotFound if the order doesn't exist or
	// is already soft-deleted.
	Update(ctx context.Context, id, customerID, status string, total float64, updatedAt time.Time) (*domain.Order, error)

	// Delete performs a logical delete and returns domain.ErrOrderNotFound
	// if the order doesn't exist or was already deleted.
	Delete(ctx context.Context, id string, deletedAt time.Time) error
}

// EventPublisher decouples usecases from the concrete message broker
// (Redis today, Kafka or NATS tomorrow — usecase code never changes).
type EventPublisher interface {
	Publish(ctx context.Context, channel string, event domain.Event) error
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

type OrderService interface {
	PlaceOrder(ctx context.Context, customerID string, total float64) (*domain.Order, error)
	GetOrder(ctx context.Context, id string) (*domain.Order, error)
	ListOrders(ctx context.Context, params ListOrdersParams) (*ListOrdersResult, error)
	UpdateOrder(ctx context.Context, id, customerID, status string, total float64) (*domain.Order, error)
	DeleteOrder(ctx context.Context, id string) error
}
