package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/lexa044/realtime-api/internal/adapter/repository"
	"github.com/lexa044/realtime-api/internal/adapter/security"
	"github.com/lexa044/realtime-api/internal/infrastructure/config"
	infradb "github.com/lexa044/realtime-api/internal/infrastructure/db"
)

// seeduser is a dev-only tool for provisioning a user (or resetting an
// existing one's password) directly against the database. This API
// deliberately has no self-serve registration endpoint, so this is the
// only way to create the first account.
//
// It hashes the password through security.BcryptHasher — the exact same
// code the running API uses to verify logins — so the hash is guaranteed
// to actually work, rather than a hand-computed or copy-pasted literal.
//
// Usage: go run ./cmd/seeduser <username> <password>
// (reads MSSQL_DSN etc. from .env or the environment, same as cmd/api)
func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: seeduser <username> <password>")
		os.Exit(1)
	}
	username, password := os.Args[1], os.Args[2]

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := infradb.NewMSSQL(ctx, cfg.MSSQLDSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mssql: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	hasher := security.NewBcryptHasher()
	hash, err := hasher.Hash(password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hash password: %v\n", err)
		os.Exit(1)
	}

	userRepo := repository.NewUserRepository(db)
	if err := userRepo.Upsert(ctx, uuid.NewString(), username, hash, time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "upsert user: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("user %q provisioned/updated\n", username)
}
