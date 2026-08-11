package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lexa044/realtime-api/internal/domain"
)

// UserRepository implements usecase.UserRepository through stored
// procedures, same as every other repository in this package.
type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	const q = `EXEC dbo.usp_User_GetByUsername @Username = @Username;`

	row := r.db.QueryRowContext(ctx, q, sql.Named("Username", username))

	var u domain.User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsActive, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("get user by username: %w", err)
	}
	return &u, nil
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	const q = `EXEC dbo.usp_User_GetByID @ID = @ID;`

	row := r.db.QueryRowContext(ctx, q, sql.Named("ID", id))

	var u domain.User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsActive, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return &u, nil
}

// Upsert inserts a new user or resets an existing one's password by
// username. Deliberately NOT part of usecase.UserRepository — ordinary
// request handling never creates or modifies a user; this only exists for
// cmd/seeduser, since the API has no self-serve registration endpoint.
func (r *UserRepository) Upsert(ctx context.Context, id, username, passwordHash string, createdAt time.Time) error {
	const q = `EXEC dbo.usp_User_Upsert
		@ID = @ID,
		@Username = @Username,
		@PasswordHash = @PasswordHash,
		@CreatedAt = @CreatedAt;`

	if _, err := r.db.ExecContext(ctx, q,
		sql.Named("ID", id),
		sql.Named("Username", username),
		sql.Named("PasswordHash", passwordHash),
		sql.Named("CreatedAt", createdAt),
	); err != nil {
		return fmt.Errorf("upsert user %s: %w", username, err)
	}
	return nil
}
