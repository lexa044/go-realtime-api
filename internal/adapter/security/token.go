package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AccessClaims is the JWT payload issued for an authenticated user.
// RegisteredClaims.Subject carries the user ID — the same shape
// VerifyAccessToken expects back.
type AccessClaims struct {
	jwt.RegisteredClaims
}

// JWTTokenIssuer implements usecase.TokenIssuer: signs access tokens with
// HS256, and generates refresh tokens as random bytes hashed with
// SHA-256 before persistence — the raw value is only ever returned to the
// client, never stored.
type JWTTokenIssuer struct {
	secret    []byte
	accessTTL time.Duration
}

func NewJWTTokenIssuer(secret []byte, accessTTL time.Duration) *JWTTokenIssuer {
	return &JWTTokenIssuer{secret: secret, accessTTL: accessTTL}
}

func (i *JWTTokenIssuer) IssueAccessToken(userID string) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(i.accessTTL)

	claims := AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

func (i *JWTTokenIssuer) NewRefreshToken() (raw string, hash string, err error) {
	buf := make([]byte, 32) // 256 bits of entropy
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(buf)
	return raw, i.HashRefreshToken(raw), nil
}

func (i *JWTTokenIssuer) HashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// VerifyAccessToken validates a JWT signed by IssueAccessToken and returns
// its subject (user ID). This is a package-level function rather than a
// usecase.TokenIssuer method, since only the HTTP auth middleware needs
// verification — usecase code never verifies its own issued tokens.
func VerifyAccessToken(secret []byte, tokenString string) (string, error) {
	claims := &AccessClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil || !token.Valid || claims.Subject == "" {
		return "", errors.New("invalid token")
	}
	return claims.Subject, nil
}
