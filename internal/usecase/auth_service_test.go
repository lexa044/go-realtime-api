package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lexa044/realtime-api/internal/domain"
	"github.com/lexa044/realtime-api/internal/usecase"
)

type fakeUserRepository struct {
	getByUsernameFn func(ctx context.Context, username string) (*domain.User, error)
	getByIDFn       func(ctx context.Context, id string) (*domain.User, error)
}

func (f *fakeUserRepository) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	if f.getByUsernameFn != nil {
		return f.getByUsernameFn(ctx, username)
	}
	return nil, errors.New("fakeUserRepository.GetByUsername not stubbed")
}

func (f *fakeUserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	if f.getByIDFn != nil {
		return f.getByIDFn(ctx, id)
	}
	return nil, errors.New("fakeUserRepository.GetByID not stubbed")
}

type fakeRefreshTokenRepository struct {
	createFn           func(ctx context.Context, t *domain.RefreshToken) error
	getByTokenHashFn   func(ctx context.Context, tokenHash string) (*domain.RefreshToken, error)
	revokeFn           func(ctx context.Context, id string, replacedBy *string, revokedAt time.Time) error
	revokeAllForUserFn func(ctx context.Context, userID string, revokedAt time.Time) error

	created           []*domain.RefreshToken
	revoked           []string
	revokedAllForUser []string
}

func (f *fakeRefreshTokenRepository) Create(ctx context.Context, t *domain.RefreshToken) error {
	f.created = append(f.created, t)
	if f.createFn != nil {
		return f.createFn(ctx, t)
	}
	return nil
}

func (f *fakeRefreshTokenRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	if f.getByTokenHashFn != nil {
		return f.getByTokenHashFn(ctx, tokenHash)
	}
	return nil, errors.New("fakeRefreshTokenRepository.GetByTokenHash not stubbed")
}

func (f *fakeRefreshTokenRepository) Revoke(ctx context.Context, id string, replacedBy *string, revokedAt time.Time) error {
	f.revoked = append(f.revoked, id)
	if f.revokeFn != nil {
		return f.revokeFn(ctx, id, replacedBy, revokedAt)
	}
	return nil
}

func (f *fakeRefreshTokenRepository) RevokeAllForUser(ctx context.Context, userID string, revokedAt time.Time) error {
	f.revokedAllForUser = append(f.revokedAllForUser, userID)
	if f.revokeAllForUserFn != nil {
		return f.revokeAllForUserFn(ctx, userID, revokedAt)
	}
	return nil
}

// fakePasswordHasher's default Compare treats "hashed:<password>" as the
// correct hash for <password> — good enough for exercising AuthService
// without a real bcrypt dependency in usecase tests.
type fakePasswordHasher struct {
	compareFn func(hash, password string) error
}

func (f *fakePasswordHasher) Hash(password string) (string, error) {
	return "hashed:" + password, nil
}

func (f *fakePasswordHasher) Compare(hash, password string) error {
	if f.compareFn != nil {
		return f.compareFn(hash, password)
	}
	if hash == "hashed:"+password {
		return nil
	}
	return errors.New("password mismatch")
}

// fakeTokenIssuer produces deterministic, inspectable tokens instead of
// real JWTs/random bytes, so tests can assert on exact values.
type fakeTokenIssuer struct {
	counter int
}

func (f *fakeTokenIssuer) IssueAccessToken(userID string) (string, time.Time, error) {
	return "access-" + userID, time.Now().UTC().Add(15 * time.Minute), nil
}

func (f *fakeTokenIssuer) NewRefreshToken() (string, string, error) {
	f.counter++
	raw := fmt.Sprintf("raw-%d", f.counter)
	return raw, f.HashRefreshToken(raw), nil
}

func (f *fakeTokenIssuer) HashRefreshToken(raw string) string {
	return "hash:" + raw
}

func TestLogin_Success(t *testing.T) {
	user := &domain.User{ID: "user-1", Username: "alice", PasswordHash: "hashed:correct-password", IsActive: true}
	users := &fakeUserRepository{
		getByUsernameFn: func(ctx context.Context, username string) (*domain.User, error) {
			if username != "alice" {
				t.Fatalf("expected username %q, got %q", "alice", username)
			}
			return user, nil
		},
	}
	tokens := &fakeRefreshTokenRepository{}
	svc := usecase.NewAuthService(users, tokens, &fakePasswordHasher{}, &fakeTokenIssuer{}, 7*24*time.Hour)

	pair, err := svc.Login(context.Background(), "alice", "correct-password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AccessToken != "access-user-1" {
		t.Fatalf("unexpected access token: %q", pair.AccessToken)
	}
	if pair.RefreshToken == "" {
		t.Fatal("expected a refresh token")
	}
	if len(tokens.created) != 1 || tokens.created[0].UserID != "user-1" {
		t.Fatalf("expected refresh token persisted for user-1, got %+v", tokens.created)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	user := &domain.User{ID: "user-1", Username: "alice", PasswordHash: "hashed:correct-password", IsActive: true}
	users := &fakeUserRepository{
		getByUsernameFn: func(ctx context.Context, username string) (*domain.User, error) { return user, nil },
	}
	svc := usecase.NewAuthService(users, &fakeRefreshTokenRepository{}, &fakePasswordHasher{}, &fakeTokenIssuer{}, time.Hour)

	_, err := svc.Login(context.Background(), "alice", "wrong-password")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_UnknownUser_ReturnsGenericError(t *testing.T) {
	users := &fakeUserRepository{
		getByUsernameFn: func(ctx context.Context, username string) (*domain.User, error) { return nil, domain.ErrUserNotFound },
	}
	svc := usecase.NewAuthService(users, &fakeRefreshTokenRepository{}, &fakePasswordHasher{}, &fakeTokenIssuer{}, time.Hour)

	_, err := svc.Login(context.Background(), "ghost", "whatever")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials (not ErrUserNotFound, so unknown username isn't distinguishable from wrong password), got %v", err)
	}
}

func TestLogin_InactiveUser(t *testing.T) {
	user := &domain.User{ID: "user-1", Username: "alice", PasswordHash: "hashed:correct-password", IsActive: false}
	users := &fakeUserRepository{
		getByUsernameFn: func(ctx context.Context, username string) (*domain.User, error) { return user, nil },
	}
	svc := usecase.NewAuthService(users, &fakeRefreshTokenRepository{}, &fakePasswordHasher{}, &fakeTokenIssuer{}, time.Hour)

	_, err := svc.Login(context.Background(), "alice", "correct-password")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for an inactive user, got %v", err)
	}
}

func TestRefresh_RotatesToken(t *testing.T) {
	existing := &domain.RefreshToken{
		ID:        "rt-1",
		UserID:    "user-1",
		TokenHash: "hash:raw-old",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}

	tokens := &fakeRefreshTokenRepository{
		getByTokenHashFn: func(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
			if tokenHash != "hash:raw-old" {
				t.Fatalf("unexpected token hash lookup: %q", tokenHash)
			}
			return existing, nil
		},
	}
	svc := usecase.NewAuthService(&fakeUserRepository{}, tokens, &fakePasswordHasher{}, &fakeTokenIssuer{}, time.Hour)

	pair, err := svc.Refresh(context.Background(), "raw-old")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AccessToken != "access-user-1" {
		t.Fatalf("unexpected access token: %q", pair.AccessToken)
	}
	if len(tokens.revoked) != 1 || tokens.revoked[0] != "rt-1" {
		t.Fatalf("expected the old token revoked, got %v", tokens.revoked)
	}
	if len(tokens.created) != 1 {
		t.Fatalf("expected a new refresh token persisted, got %d", len(tokens.created))
	}
}

func TestRefresh_ReusedRevokedToken_RevokesAllForUser(t *testing.T) {
	revokedAt := time.Now().UTC().Add(-time.Minute)
	existing := &domain.RefreshToken{
		ID:        "rt-1",
		UserID:    "user-1",
		TokenHash: "hash:raw-old",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		RevokedAt: &revokedAt,
	}

	tokens := &fakeRefreshTokenRepository{
		getByTokenHashFn: func(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) { return existing, nil },
	}
	svc := usecase.NewAuthService(&fakeUserRepository{}, tokens, &fakePasswordHasher{}, &fakeTokenIssuer{}, time.Hour)

	_, err := svc.Refresh(context.Background(), "raw-old")
	if !errors.Is(err, domain.ErrRefreshTokenInvalid) {
		t.Fatalf("expected ErrRefreshTokenInvalid, got %v", err)
	}
	if len(tokens.revokedAllForUser) != 1 || tokens.revokedAllForUser[0] != "user-1" {
		t.Fatalf("expected RevokeAllForUser called for user-1, got %v", tokens.revokedAllForUser)
	}
}

func TestRefresh_ExpiredToken(t *testing.T) {
	existing := &domain.RefreshToken{
		ID:        "rt-1",
		UserID:    "user-1",
		TokenHash: "hash:raw-old",
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	}

	tokens := &fakeRefreshTokenRepository{
		getByTokenHashFn: func(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) { return existing, nil },
	}
	svc := usecase.NewAuthService(&fakeUserRepository{}, tokens, &fakePasswordHasher{}, &fakeTokenIssuer{}, time.Hour)

	_, err := svc.Refresh(context.Background(), "raw-old")
	if !errors.Is(err, domain.ErrRefreshTokenInvalid) {
		t.Fatalf("expected ErrRefreshTokenInvalid for an expired token, got %v", err)
	}
}

func TestRefresh_UnknownToken(t *testing.T) {
	tokens := &fakeRefreshTokenRepository{
		getByTokenHashFn: func(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
			return nil, domain.ErrRefreshTokenInvalid
		},
	}
	svc := usecase.NewAuthService(&fakeUserRepository{}, tokens, &fakePasswordHasher{}, &fakeTokenIssuer{}, time.Hour)

	_, err := svc.Refresh(context.Background(), "never-issued")
	if !errors.Is(err, domain.ErrRefreshTokenInvalid) {
		t.Fatalf("expected ErrRefreshTokenInvalid, got %v", err)
	}
}

func TestLogout_RevokesToken(t *testing.T) {
	existing := &domain.RefreshToken{
		ID:        "rt-1",
		UserID:    "user-1",
		TokenHash: "hash:raw-1",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}

	tokens := &fakeRefreshTokenRepository{
		getByTokenHashFn: func(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) { return existing, nil },
	}
	svc := usecase.NewAuthService(&fakeUserRepository{}, tokens, &fakePasswordHasher{}, &fakeTokenIssuer{}, time.Hour)

	if err := svc.Logout(context.Background(), "raw-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens.revoked) != 1 || tokens.revoked[0] != "rt-1" {
		t.Fatalf("expected the token revoked, got %v", tokens.revoked)
	}
}

func TestLogout_UnknownToken_IsNotAnError(t *testing.T) {
	tokens := &fakeRefreshTokenRepository{
		getByTokenHashFn: func(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
			return nil, domain.ErrRefreshTokenInvalid
		},
	}
	svc := usecase.NewAuthService(&fakeUserRepository{}, tokens, &fakePasswordHasher{}, &fakeTokenIssuer{}, time.Hour)

	// Logout is intentionally silent about whether the token was ever
	// valid, so an unknown token still reports success.
	if err := svc.Logout(context.Background(), "never-existed"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
