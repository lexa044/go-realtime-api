package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lexa044/realtime-api/internal/domain"
)

// RefreshTokenRepository implements usecase.RefreshTokenRepository through
// stored procedures.
type RefreshTokenRepository struct {
	db *sql.DB
}

func NewRefreshTokenRepository(db *sql.DB) *RefreshTokenRepository {
	return &RefreshTokenRepository{db: db}
}

func (r *RefreshTokenRepository) Create(ctx context.Context, t *domain.RefreshToken) error {
	const q = `EXEC dbo.usp_RefreshToken_Create
		@ID = @ID,
		@UserID = @UserID,
		@TokenHash = @TokenHash,
		@ExpiresAt = @ExpiresAt,
		@CreatedAt = @CreatedAt;`

	if _, err := r.db.ExecContext(ctx, q,
		sql.Named("ID", t.ID),
		sql.Named("UserID", t.UserID),
		sql.Named("TokenHash", t.TokenHash),
		sql.Named("ExpiresAt", t.ExpiresAt),
		sql.Named("CreatedAt", t.CreatedAt),
	); err != nil {
		return fmt.Errorf("create refresh token: %w", err)
	}
	return nil
}

// GetByTokenHash deliberately does NOT filter out revoked tokens — the
// caller (AuthService.Refresh) needs to see a revoked token in order to
// detect reuse and react to it, not just be told "not found".
func (r *RefreshTokenRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	const q = `EXEC dbo.usp_RefreshToken_GetByTokenHash @TokenHash = @TokenHash;`

	row := r.db.QueryRowContext(ctx, q, sql.Named("TokenHash", tokenHash))

	var (
		t          domain.RefreshToken
		revokedAt  sql.NullTime
		replacedBy sql.NullString
	)
	if err := row.Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &revokedAt, &replacedBy, &t.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrRefreshTokenInvalid
		}
		return nil, fmt.Errorf("get refresh token: %w", err)
	}

	t.RevokedAt = nullTimeToPtr(revokedAt)
	if replacedBy.Valid {
		v := replacedBy.String
		t.ReplacedBy = &v
	}
	return &t, nil
}

func (r *RefreshTokenRepository) Revoke(ctx context.Context, id string, replacedBy *string, revokedAt time.Time) error {
	const q = `EXEC dbo.usp_RefreshToken_Revoke
		@ID = @ID,
		@ReplacedBy = @ReplacedBy,
		@RevokedAt = @RevokedAt;`

	var replacedByParam any
	if replacedBy != nil {
		replacedByParam = *replacedBy
	}

	if _, err := r.db.ExecContext(ctx, q,
		sql.Named("ID", id),
		sql.Named("ReplacedBy", replacedByParam),
		sql.Named("RevokedAt", revokedAt),
	); err != nil {
		return fmt.Errorf("revoke refresh token %s: %w", id, err)
	}
	return nil
}

func (r *RefreshTokenRepository) RevokeAllForUser(ctx context.Context, userID string, revokedAt time.Time) error {
	const q = `EXEC dbo.usp_RefreshToken_RevokeAllForUser @UserID = @UserID, @RevokedAt = @RevokedAt;`

	if _, err := r.db.ExecContext(ctx, q,
		sql.Named("UserID", userID),
		sql.Named("RevokedAt", revokedAt),
	); err != nil {
		return fmt.Errorf("revoke all refresh tokens for user %s: %w", userID, err)
	}
	return nil
}
