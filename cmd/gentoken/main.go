package main

import (
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// gentoken is a dev-only helper: it prints a signed JWT so you can exercise
// the API and websocket endpoint without standing up a real identity
// provider. Do not ship this — it exists purely for local/testing use.
//
// Usage: JWT_SECRET=dev-secret-change-me go run ./cmd/gentoken <user-id>
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

	claims := jwt.RegisteredClaims{
		Subject:   os.Args[1],
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign error:", err)
		os.Exit(1)
	}
	fmt.Println(signed)
}
