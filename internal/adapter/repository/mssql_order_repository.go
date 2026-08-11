package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lexa044/realtime-api/internal/domain"
)

// OrderRepository implements usecase.OrderRepository entirely through
// stored procedures — no dynamic SQL is built from Go strings anywhere in
// this file. That keeps query plans stable and cacheable, lets DBAs
// audit/tune data access independently of application deploys, and limits
// the injection surface to well-typed, named parameters.
type OrderRepository struct {
	db *sql.DB
}

func NewOrderRepository(db *sql.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

func (r *OrderRepository) Create(ctx context.Context, o *domain.Order) error {
	const q = `EXEC dbo.usp_Order_Create
		@ID = @ID,
		@CustomerID = @CustomerID,
		@Status = @Status,
		@Total = @Total,
		@CurrencyCode = @CurrencyCode,
		@CreatedAt = @CreatedAt,
		@CreatedBy = @CreatedBy,
		@UpdatedBy = @UpdatedBy;`

	if _, err := r.db.ExecContext(ctx, q,
		sql.Named("ID", o.ID),
		sql.Named("CustomerID", o.CustomerID),
		sql.Named("Status", o.Status.String()),
		sql.Named("Total", o.Total.Amount()),
		sql.Named("CurrencyCode", o.Total.Currency()),
		sql.Named("CreatedAt", o.CreatedAt),
		sql.Named("CreatedBy", o.CreatedBy),
		sql.Named("UpdatedBy", o.UpdatedBy),
	); err != nil {
		return fmt.Errorf("create order %s: %w", o.ID, err)
	}
	return nil
}

func (r *OrderRepository) GetByID(ctx context.Context, id string) (*domain.Order, error) {
	const q = `EXEC dbo.usp_Order_GetByID @ID = @ID;`

	row := r.db.QueryRowContext(ctx, q, sql.Named("ID", id))
	order, err := scanOrder(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrOrderNotFound
		}
		return nil, fmt.Errorf("get order %s: %w", id, err)
	}
	return order, nil
}

// List calls dbo.usp_Order_List, which returns one result set: each row
// carries a TotalCount column (COUNT(*) OVER()) alongside the page of
// data, so the total matching row count comes back in the same round trip
// instead of a separate COUNT(*) query.
func (r *OrderRepository) List(ctx context.Context, customerID string, page, pageSize int) ([]domain.Order, int, error) {
	const q = `EXEC dbo.usp_Order_List
		@CustomerID = @CustomerID,
		@PageNumber = @PageNumber,
		@PageSize = @PageSize;`

	// An empty filter should mean "no filter", not "match rows where
	// CustomerID = ''" — pass SQL NULL so the proc's
	// (@CustomerID IS NULL OR CustomerID = @CustomerID) clause skips it.
	var customerIDParam any
	if customerID != "" {
		customerIDParam = customerID
	}

	rows, err := r.db.QueryContext(ctx, q,
		sql.Named("CustomerID", customerIDParam),
		sql.Named("PageNumber", page),
		sql.Named("PageSize", pageSize),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()

	var (
		orders []domain.Order
		total  int
	)
	for rows.Next() {
		order, err := scanOrder(rows.Scan, &total)
		if err != nil {
			return nil, 0, fmt.Errorf("list orders: scan row: %w", err)
		}
		orders = append(orders, *order)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list orders: %w", err)
	}
	return orders, total, nil
}

// Update calls dbo.usp_Order_Update, which updates and re-selects the row
// in one round trip. The proc's WHERE clause excludes soft-deleted rows,
// so it's a no-op — zero rows updated, zero rows returned — for both a
// missing ID and an already-deleted one. An empty result set here always
// means domain.ErrOrderNotFound.
func (r *OrderRepository) Update(ctx context.Context, id, customerID string, status domain.OrderStatus, total domain.Money, updatedBy string, updatedAt time.Time) (*domain.Order, error) {
	const q = `EXEC dbo.usp_Order_Update
		@ID = @ID,
		@CustomerID = @CustomerID,
		@Status = @Status,
		@Total = @Total,
		@CurrencyCode = @CurrencyCode,
		@UpdatedBy = @UpdatedBy,
		@UpdatedAt = @UpdatedAt;`

	row := r.db.QueryRowContext(ctx, q,
		sql.Named("ID", id),
		sql.Named("CustomerID", customerID),
		sql.Named("Status", status.String()),
		sql.Named("Total", total.Amount()),
		sql.Named("CurrencyCode", total.Currency()),
		sql.Named("UpdatedBy", updatedBy),
		sql.Named("UpdatedAt", updatedAt),
	)

	order, err := scanOrder(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrOrderNotFound
		}
		return nil, fmt.Errorf("update order %s: %w", id, err)
	}
	return order, nil
}

// Delete calls dbo.usp_Order_Delete, a logical delete: the proc flags
// IsDeleted rather than removing the row, records deletedBy as UpdatedBy
// (a delete is still a modification of the row), and returns the number
// of rows its UPDATE actually touched so we can tell "deleted" apart from
// "already gone".
func (r *OrderRepository) Delete(ctx context.Context, id, deletedBy string, deletedAt time.Time) error {
	const q = `EXEC dbo.usp_Order_Delete @ID = @ID, @UpdatedBy = @UpdatedBy, @DeletedAt = @DeletedAt;`

	row := r.db.QueryRowContext(ctx, q,
		sql.Named("ID", id),
		sql.Named("UpdatedBy", deletedBy),
		sql.Named("DeletedAt", deletedAt),
	)

	var rowsAffected int
	if err := row.Scan(&rowsAffected); err != nil {
		return fmt.Errorf("delete order %s: %w", id, err)
	}
	if rowsAffected == 0 {
		return domain.ErrOrderNotFound
	}
	return nil
}

// scanOrder reads one row shaped like the result sets returned by
// usp_Order_GetByID/List/Update into a domain.Order, translating the raw
// Status/Total/CurrencyCode columns into their validated domain types
// (OrderStatus, Money) via the same constructors the rest of the app uses
// — a row that somehow contains an invalid status or amount surfaces as an
// error here rather than silently entering the domain unchecked.
//
// extra accepts additional destination pointers appended after the fixed
// Order columns, for callers (List) whose query returns extra columns like
// TotalCount.
func scanOrder(scan func(dest ...any) error, extra ...any) (*domain.Order, error) {
	var (
		o            domain.Order
		statusRaw    string
		amount       float64
		currencyCode string
		updatedAt    sql.NullTime
	)

	dest := []any{
		&o.ID, &o.CustomerID, &statusRaw, &amount, &currencyCode,
		&o.CreatedAt, &updatedAt, &o.IsDeleted, &o.CreatedBy, &o.UpdatedBy,
	}
	dest = append(dest, extra...)

	if err := scan(dest...); err != nil {
		return nil, err
	}

	status, err := domain.ParseOrderStatus(statusRaw)
	if err != nil {
		return nil, fmt.Errorf("order %s: %w", o.ID, err)
	}
	total, err := domain.NewMoney(amount, currencyCode)
	if err != nil {
		return nil, fmt.Errorf("order %s: %w", o.ID, err)
	}

	o.Status = status
	o.Total = total
	o.UpdatedAt = nullTimeToPtr(updatedAt)
	return &o, nil
}

// nullTimeToPtr converts a nullable DB column into domain's preferred
// representation: nil means "never set", a non-nil pointer means "set to
// this value". Scanning directly into *time.Time fails on SQL NULL, so
// every nullable timestamp column goes through sql.NullTime first.
func nullTimeToPtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	t := nt.Time
	return &t
}
