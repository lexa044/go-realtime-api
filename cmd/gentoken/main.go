package main

import (
	"fmt"
	"os"
	"time"

	"github.com/lexa044/realtime-api/internal/adapter/security"
)

// gentoken is a dev-only helper: it prints a signed access token so you
// can exercise the API and websocket endpoint without going through a
// real /auth/login call. Unlike /auth/login, it accepts ANY user id with
// no password check at all — do not ship this or expose it to end users.
//
// It signs through security.JWTTokenIssuer, the exact code path the
// running API uses to issue real tokens, so a token minted here is
// indistinguishable from one issued by /auth/login (same claims shape,
// same signing method) and can never silently drift out of sync with it.
//
// Usage: JWT_SECRET=dev-secret-change-me go run ./cmd/gentoken <user-id>
// Optionally set ACCESS_TOKEN_TTL (e.g. "15m") to match the running API;
// defaults to 15m if unset. Deliberately reads only these two env vars
// directly (not config.Load) so it stays usable without MSSQL/Redis
// configured.
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: gentoken <user-id>")
		os.Exit(1)
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET env var is required")
		os.Exit(1)
	}

	ttl := 15 * time.Minute
	if raw := os.Getenv("ACCESS_TOKEN_TTL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid ACCESS_TOKEN_TTL: %v\n", err)
			os.Exit(1)
		}
		ttl = parsed
	}

	issuer := security.NewJWTTokenIssuer([]byte(secret), ttl)
	token, expiresAt, err := issuer.IssueAccessToken(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign error:", err)
		os.Exit(1)
	}

	fmt.Println(token)
	fmt.Fprintf(os.Stderr, "expires at %s\n", expiresAt.Format(time.RFC3339))
}
