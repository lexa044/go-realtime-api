package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lexa044/realtime-api/internal/domain"
	"github.com/lexa044/realtime-api/internal/usecase"
)

// fakeOrderRepository is a hand-rolled test double implementing
// usecase.OrderRepository. The port is small and explicit enough that a
// mocking framework would just add indirection.
type fakeOrderRepository struct {
	createFn func(ctx context.Context, o *domain.Order) error
	getFn    func(ctx context.Context, id string) (*domain.Order, error)
	listFn   func(ctx context.Context, customerID string, page, pageSize int) ([]domain.Order, int, error)
	updateFn func(ctx context.Context, id, customerID string, status domain.OrderStatus, total domain.Money, updatedAt time.Time) (*domain.Order, error)
	deleteFn func(ctx context.Context, id string, deletedAt time.Time) error

	created []*domain.Order
}

func (f *fakeOrderRepository) Create(ctx context.Context, o *domain.Order) error {
	f.created = append(f.created, o)
	if f.createFn != nil {
		return f.createFn(ctx, o)
	}
	return nil
}

func (f *fakeOrderRepository) GetByID(ctx context.Context, id string) (*domain.Order, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return nil, errors.New("fakeOrderRepository.GetByID not stubbed")
}

func (f *fakeOrderRepository) List(ctx context.Context, customerID string, page, pageSize int) ([]domain.Order, int, error) {
	if f.listFn != nil {
		return f.listFn(ctx, customerID, page, pageSize)
	}
	return nil, 0, errors.New("fakeOrderRepository.List not stubbed")
}

func (f *fakeOrderRepository) Update(ctx context.Context, id, customerID string, status domain.OrderStatus, total domain.Money, updatedAt time.Time) (*domain.Order, error) {
	if f.updateFn != nil {
		return f.updateFn(ctx, id, customerID, status, total, updatedAt)
	}
	return nil, errors.New("fakeOrderRepository.Update not stubbed")
}

func (f *fakeOrderRepository) Delete(ctx context.Context, id string, deletedAt time.Time) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, id, deletedAt)
	}
	return errors.New("fakeOrderRepository.Delete not stubbed")
}

// fakeEventPublisher is a test double for usecase.EventPublisher.
type fakeEventPublisher struct {
	publishFn func(ctx context.Context, channel string, event domain.Event) error

	published []domain.Event
	channels  []string
}

func (f *fakeEventPublisher) Publish(ctx context.Context, channel string, event domain.Event) error {
	f.channels = append(f.channels, channel)
	f.published = append(f.published, event)
	if f.publishFn != nil {
		return f.publishFn(ctx, channel, event)
	}
	return nil
}

func TestPlaceOrder_Success(t *testing.T) {
	repo := &fakeOrderRepository{}
	pub := &fakeEventPublisher{}
	svc := usecase.NewOrderService(repo, pub)

	total := domain.MustMoney(42.5, "USD")
	order, err := svc.PlaceOrder(context.Background(), "cust-1", total)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.CustomerID != "cust-1" {
		t.Fatalf("unexpected customer id: %q", order.CustomerID)
	}
	if order.Total.Amount() != 42.5 || order.Total.Currency() != "USD" {
		t.Fatalf("unexpected total: %v", order.Total)
	}
	if order.Status != domain.OrderStatusPending {
		t.Fatalf("expected status %q, got %q", domain.OrderStatusPending, order.Status)
	}
	if order.ID == "" {
		t.Fatal("expected a generated ID")
	}

	if len(repo.created) != 1 {
		t.Fatalf("expected 1 repo.Create call, got %d", len(repo.created))
	}
	if repo.created[0].ID != order.ID {
		t.Fatal("repository received a different order than the one returned")
	}

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(pub.published))
	}
	if pub.channels[0] != usecase.OrderEventsChannel {
		t.Fatalf("expected channel %q, got %q", usecase.OrderEventsChannel, pub.channels[0])
	}
	if pub.published[0].Type != domain.EventOrderCreated {
		t.Fatalf("expected event type %q, got %q", domain.EventOrderCreated, pub.published[0].Type)
	}
	payload, ok := pub.published[0].Payload.(*domain.Order)
	if !ok || payload.ID != order.ID {
		t.Fatal("expected the published event's payload to be the created order")
	}
}

func TestPlaceOrder_RepositoryError_SkipsPublish(t *testing.T) {
	wantErr := errors.New("db down")
	repo := &fakeOrderRepository{
		createFn: func(ctx context.Context, o *domain.Order) error { return wantErr },
	}
	pub := &fakeEventPublisher{}
	svc := usecase.NewOrderService(repo, pub)

	_, err := svc.PlaceOrder(context.Background(), "cust-1", domain.MustMoney(10, "USD"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected repository error to propagate, got %v", err)
	}
	if len(pub.published) != 0 {
		t.Fatalf("expected no publish attempt when persistence fails, got %d", len(pub.published))
	}
}

func TestPlaceOrder_PublishError_StillSucceeds(t *testing.T) {
	// Notification failures must not roll back a persisted order — this
	// is the "fire-and-forget" contract documented on the service.
	repo := &fakeOrderRepository{}
	pub := &fakeEventPublisher{
		publishFn: func(ctx context.Context, channel string, event domain.Event) error {
			return errors.New("redis unreachable")
		},
	}
	svc := usecase.NewOrderService(repo, pub)

	order, err := svc.PlaceOrder(context.Background(), "cust-1", domain.MustMoney(10, "USD"))
	if err != nil {
		t.Fatalf("expected PlaceOrder to succeed even if publish fails, got %v", err)
	}
	if order == nil {
		t.Fatal("expected a non-nil order")
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected order to still be persisted, repo.Create called %d times", len(repo.created))
	}
}

func TestGetOrder_DelegatesToRepository(t *testing.T) {
	want := &domain.Order{ID: "abc", CustomerID: "cust-1"}
	repo := &fakeOrderRepository{
		getFn: func(ctx context.Context, id string) (*domain.Order, error) {
			if id != "abc" {
				t.Fatalf("expected repo.GetByID called with %q, got %q", "abc", id)
			}
			return want, nil
		},
	}
	svc := usecase.NewOrderService(repo, &fakeEventPublisher{})

	got, err := svc.GetOrder(context.Background(), "abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatal("expected the same order returned by the repository")
	}
}

func TestGetOrder_NotFound(t *testing.T) {
	repo := &fakeOrderRepository{
		getFn: func(ctx context.Context, id string) (*domain.Order, error) { return nil, domain.ErrOrderNotFound },
	}
	svc := usecase.NewOrderService(repo, &fakeEventPublisher{})

	_, err := svc.GetOrder(context.Background(), "missing")
	if !errors.Is(err, domain.ErrOrderNotFound) {
		t.Fatalf("expected ErrOrderNotFound, got %v", err)
	}
}

func TestListOrders_ClampsPaging(t *testing.T) {
	var gotCustomerID string
	var gotPage, gotPageSize int
	repo := &fakeOrderRepository{
		listFn: func(ctx context.Context, customerID string, page, pageSize int) ([]domain.Order, int, error) {
			gotCustomerID, gotPage, gotPageSize = customerID, page, pageSize
			return []domain.Order{{ID: "1"}}, 1, nil
		},
	}
	svc := usecase.NewOrderService(repo, &fakeEventPublisher{})

	result, err := svc.ListOrders(context.Background(), usecase.ListOrdersParams{
		CustomerID: "cust-1",
		Page:       0,     // should clamp to 1
		PageSize:   10000, // should clamp to 200
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCustomerID != "cust-1" {
		t.Fatalf("expected customerID %q passed through, got %q", "cust-1", gotCustomerID)
	}
	if gotPage != 1 {
		t.Fatalf("expected page clamped to 1, got %d", gotPage)
	}
	if gotPageSize != 200 {
		t.Fatalf("expected page size clamped to 200, got %d", gotPageSize)
	}
	if result.TotalCount != 1 || len(result.Orders) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestListOrders_DefaultsPageSize(t *testing.T) {
	var gotPageSize int
	repo := &fakeOrderRepository{
		listFn: func(ctx context.Context, customerID string, page, pageSize int) ([]domain.Order, int, error) {
			gotPageSize = pageSize
			return nil, 0, nil
		},
	}
	svc := usecase.NewOrderService(repo, &fakeEventPublisher{})

	if _, err := svc.ListOrders(context.Background(), usecase.ListOrdersParams{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPageSize != 20 {
		t.Fatalf("expected default page size 20, got %d", gotPageSize)
	}
}

func TestUpdateOrder_PublishesEvent(t *testing.T) {
	updated := &domain.Order{
		ID:         "abc",
		CustomerID: "cust-2",
		Status:     domain.OrderStatusShipped,
		Total:      domain.MustMoney(99, "USD"),
	}
	repo := &fakeOrderRepository{
		updateFn: func(ctx context.Context, id, customerID string, status domain.OrderStatus, total domain.Money, updatedAt time.Time) (*domain.Order, error) {
			return updated, nil
		},
	}
	pub := &fakeEventPublisher{}
	svc := usecase.NewOrderService(repo, pub)

	got, err := svc.UpdateOrder(context.Background(), "abc", "cust-2", domain.OrderStatusShipped, domain.MustMoney(99, "USD"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != updated {
		t.Fatal("expected the order returned by the repository")
	}
	if len(pub.published) != 1 || pub.published[0].Type != domain.EventOrderUpdated {
		t.Fatalf("expected one order.updated event, got %+v", pub.published)
	}
}

func TestUpdateOrder_NotFound_SkipsPublish(t *testing.T) {
	repo := &fakeOrderRepository{
		updateFn: func(ctx context.Context, id, customerID string, status domain.OrderStatus, total domain.Money, updatedAt time.Time) (*domain.Order, error) {
			return nil, domain.ErrOrderNotFound
		},
	}
	pub := &fakeEventPublisher{}
	svc := usecase.NewOrderService(repo, pub)

	_, err := svc.UpdateOrder(context.Background(), "missing", "cust-1", domain.OrderStatusShipped, domain.MustMoney(10, "USD"))
	if !errors.Is(err, domain.ErrOrderNotFound) {
		t.Fatalf("expected ErrOrderNotFound, got %v", err)
	}
	if len(pub.published) != 0 {
		t.Fatal("expected no publish attempt when update fails")
	}
}

func TestDeleteOrder_PublishesEvent(t *testing.T) {
	repo := &fakeOrderRepository{
		deleteFn: func(ctx context.Context, id string, deletedAt time.Time) error { return nil },
	}
	pub := &fakeEventPublisher{}
	svc := usecase.NewOrderService(repo, pub)

	if err := svc.DeleteOrder(context.Background(), "abc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pub.published) != 1 || pub.published[0].Type != domain.EventOrderDeleted {
		t.Fatalf("expected one order.deleted event, got %+v", pub.published)
	}
}

func TestDeleteOrder_NotFound_SkipsPublish(t *testing.T) {
	repo := &fakeOrderRepository{
		deleteFn: func(ctx context.Context, id string, deletedAt time.Time) error { return domain.ErrOrderNotFound },
	}
	pub := &fakeEventPublisher{}
	svc := usecase.NewOrderService(repo, pub)

	err := svc.DeleteOrder(context.Background(), "missing")
	if !errors.Is(err, domain.ErrOrderNotFound) {
		t.Fatalf("expected ErrOrderNotFound, got %v", err)
	}
	if len(pub.published) != 0 {
		t.Fatal("expected no publish attempt when delete fails")
	}
}
