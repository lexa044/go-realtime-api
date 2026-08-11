package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/lexa044/realtime-api/internal/domain"
)

type authService struct {
	users         UserRepository
	refreshTokens RefreshTokenRepository
	hasher        PasswordHasher
	tokens        TokenIssuer
	refreshTTL    time.Duration
}

func NewAuthService(
	users UserRepository,
	refreshTokens RefreshTokenRepository,
	hasher PasswordHasher,
	tokens TokenIssuer,
	refreshTTL time.Duration,
) AuthService {
	return &authService{
		users:         users,
		refreshTokens: refreshTokens,
		hasher:        hasher,
		tokens:        tokens,
		refreshTTL:    refreshTTL,
	}
}

func (s *authService) Login(ctx context.Context, username, password string) (*TokenPair, error) {
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			// Deliberately the same error as a wrong password: the
			// caller can't tell "no such user" from "wrong password".
			return nil, domain.ErrInvalidCredentials
		}
		return nil, err
	}
	if !user.IsActive {
		return nil, domain.ErrInvalidCredentials
	}
	if err := s.hasher.Compare(user.PasswordHash, password); err != nil {
		return nil, domain.ErrInvalidCredentials
	}

	pair, _, err := s.issueTokenPair(ctx, user.ID)
	return pair, err
}

func (s *authService) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	hash := s.tokens.HashRefreshToken(refreshToken)

	existing, err := s.refreshTokens.GetByTokenHash(ctx, hash)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	if existing.RevokedAt != nil {
		// A revoked token being presented again is a strong signal it
		// was stolen: revoke every outstanding token for this user, so
		// both the legitimate holder and whoever replayed the old token
		// are forced to log in again.
		_ = s.refreshTokens.RevokeAllForUser(ctx, existing.UserID, now)
		return nil, domain.ErrRefreshTokenInvalid
	}

	if now.After(existing.ExpiresAt) {
		return nil, domain.ErrRefreshTokenInvalid
	}

	pair, newID, err := s.issueTokenPair(ctx, existing.UserID)
	if err != nil {
		return nil, err
	}

	// Rotate: the presented token is now spent, linked to its replacement
	// so the chain can be traced (and so reuse of THIS token is detected
	// on any future call).
	if err := s.refreshTokens.Revoke(ctx, existing.ID, &newID, now); err != nil {
		return nil, err
	}

	return pair, nil
}

func (s *authService) Logout(ctx context.Context, refreshToken string) error {
	hash := s.tokens.HashRefreshToken(refreshToken)

	existing, err := s.refreshTokens.GetByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, domain.ErrRefreshTokenInvalid) {
			// Logout never reports whether the token was ever valid.
			return nil
		}
		return err
	}
	if existing.RevokedAt != nil {
		return nil
	}

	return s.refreshTokens.Revoke(ctx, existing.ID, nil, time.Now().UTC())
}

// issueTokenPair mints a fresh access+refresh token pair for userID and
// persists the refresh token. Returns the new refresh token's ID alongside
// the pair, since Refresh needs it to link the rotation chain
// (ReplacedBy) on the token being replaced.
func (s *authService) issueTokenPair(ctx context.Context, userID string) (*TokenPair, string, error) {
	accessToken, accessExpiresAt, err := s.tokens.IssueAccessToken(userID)
	if err != nil {
		return nil, "", err
	}

	rawRefresh, refreshHash, err := s.tokens.NewRefreshToken()
	if err != nil {
		return nil, "", err
	}

	now := time.Now().UTC()
	refreshExpiresAt := now.Add(s.refreshTTL)

	rt := &domain.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		TokenHash: refreshHash,
		ExpiresAt: refreshExpiresAt,
		CreatedAt: now,
	}
	if err := s.refreshTokens.Create(ctx, rt); err != nil {
		return nil, "", err
	}

	return &TokenPair{
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshToken:          rawRefresh,
		RefreshTokenExpiresAt: refreshExpiresAt,
	}, rt.ID, nil
}
