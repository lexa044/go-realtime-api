package usecase

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/lexa044/realtime-api/internal/domain"
)

const OrderEventsChannel = "events:orders"

const (
	defaultPageSize = 20
	maxPageSize     = 200
)

type orderService struct {
	repo      OrderRepository
	publisher EventPublisher
}

func NewOrderService(repo OrderRepository, publisher EventPublisher) OrderService {
	return &orderService{repo: repo, publisher: publisher}
}

func (s *orderService) PlaceOrder(ctx context.Context, actorUserID, customerID string, total domain.Money) (*domain.Order, error) {
	order := &domain.Order{
		ID:         uuid.NewString(),
		CustomerID: customerID,
		Status:     domain.OrderStatusPending,
		Total:      total,
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  actorUserID,
		UpdatedBy:  actorUserID,
	}

	if err := s.repo.Create(ctx, order); err != nil {
		return nil, err
	}

	s.publish(ctx, domain.EventOrderCreated, order)
	return order, nil
}

func (s *orderService) GetOrder(ctx context.Context, id string) (*domain.Order, error) {
	return s.repo.GetByID(ctx, id)
}

// ListOrders clamps paging inputs before delegating to the repository, so
// a caller sending page=0 or an absurd page_size can't produce a query the
// stored procedure wasn't designed for.
func (s *orderService) ListOrders(ctx context.Context, params ListOrdersParams) (*ListOrdersResult, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}

	pageSize := params.PageSize
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	orders, total, err := s.repo.List(ctx, params.CustomerID, page, pageSize)
	if err != nil {
		return nil, err
	}

	return &ListOrdersResult{
		Orders:     orders,
		Page:       page,
		PageSize:   pageSize,
		TotalCount: total,
	}, nil
}

func (s *orderService) UpdateOrder(ctx context.Context, actorUserID, id, customerID string, status domain.OrderStatus, total domain.Money) (*domain.Order, error) {
	order, err := s.repo.Update(ctx, id, customerID, status, total, actorUserID, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	s.publish(ctx, domain.EventOrderUpdated, order)
	return order, nil
}

// DeleteOrder is a logical delete — the repository flags the row rather
// than removing it, so order history survives for audit/reporting.
func (s *orderService) DeleteOrder(ctx context.Context, actorUserID, id string) error {
	if err := s.repo.Delete(ctx, id, actorUserID, time.Now().UTC()); err != nil {
		return err
	}

	s.publish(ctx, domain.EventOrderDeleted, map[string]string{"id": id})
	return nil
}

// publish is fire-and-forget from the caller's point of view: a
// notification failure shouldn't roll back a persisted write. If delivery
// guarantees matter later, switch to an outbox table + relay instead of
// publishing inline here.
func (s *orderService) publish(ctx context.Context, eventType string, payload any) {
	event := domain.Event{
		ID:         uuid.NewString(),
		Type:       eventType,
		Payload:    payload,
		OccurredAt: time.Now().UTC(),
	}
	if err := s.publisher.Publish(ctx, OrderEventsChannel, event); err != nil {
		log.Printf("publish %s event: %v", eventType, err)
	}
}
