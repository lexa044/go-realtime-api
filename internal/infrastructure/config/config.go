package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr          string
	MSSQLDSN          string
	RedisAddr         string
	RedisPass         string
	JWTSecret         string
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	LoginRateLimit    int           // max /auth/login attempts per LoginRateWindow, per client IP
	LoginRateWindow   time.Duration
	AuthRateLimit     int           // max /auth/refresh + /auth/logout calls per AuthRateWindow, per client IP
	AuthRateWindow    time.Duration
}

// Load builds the Config from environment variables, optionally seeded by a
// local .env file. MSSQLDSN and JWTSecret have no safe default and are
// required — Load fails fast rather than letting the app start half
// configured.
func Load() (*Config, error) {
	// Attempt to load .env from the working directory. The file is optional;
	// if it doesn't exist we silently continue with the real environment.
	if err := loadEnvFile(".env"); err != nil {
		return nil, fmt.Errorf("reading .env file: %w", err)
	}

	accessTTL, err := time.ParseDuration(getenv("ACCESS_TOKEN_TTL", "15m"))
	if err != nil {
		return nil, fmt.Errorf("invalid ACCESS_TOKEN_TTL: %w", err)
	}
	refreshTTL, err := time.ParseDuration(getenv("REFRESH_TOKEN_TTL", "168h"))
	if err != nil {
		return nil, fmt.Errorf("invalid REFRESH_TOKEN_TTL: %w", err)
	}

	loginRateLimit, err := strconv.Atoi(getenv("LOGIN_RATE_LIMIT", "5"))
	if err != nil {
		return nil, fmt.Errorf("invalid LOGIN_RATE_LIMIT: %w", err)
	}
	loginRateWindow, err := time.ParseDuration(getenv("LOGIN_RATE_WINDOW", "1m"))
	if err != nil {
		return nil, fmt.Errorf("invalid LOGIN_RATE_WINDOW: %w", err)
	}
	authRateLimit, err := strconv.Atoi(getenv("AUTH_RATE_LIMIT", "20"))
	if err != nil {
		return nil, fmt.Errorf("invalid AUTH_RATE_LIMIT: %w", err)
	}
	authRateWindow, err := time.ParseDuration(getenv("AUTH_RATE_WINDOW", "1m"))
	if err != nil {
		return nil, fmt.Errorf("invalid AUTH_RATE_WINDOW: %w", err)
	}

	cfg := &Config{
		HTTPAddr:        getenv("HTTP_ADDR", ":8080"),
		MSSQLDSN:        getenv("MSSQL_DSN", ""),
		RedisAddr:       getenv("REDIS_ADDR", "localhost:6379"),
		RedisPass:       getenv("REDIS_PASSWORD", ""),
		JWTSecret:       getenv("JWT_SECRET", ""),
		AccessTokenTTL:  accessTTL,
		RefreshTokenTTL: refreshTTL,
		LoginRateLimit:  loginRateLimit,
		LoginRateWindow: loginRateWindow,
		AuthRateLimit:   authRateLimit,
		AuthRateWindow:  authRateWindow,
	}

	if cfg.MSSQLDSN == "" {
		return nil, fmt.Errorf("MSSQLDSN environment variable is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET environment variable is required")
	}

	return cfg, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// loadEnvFile reads a .env file and sets any keys that are NOT already present
// in the real environment. This means actual env vars always win, and the .env
// file acts only as a convenient default for local development.
//
// Supported syntax:
//
//	KEY=value
//	KEY="value with spaces"
//	KEY='literal value'
//	export KEY=value        (the "export " prefix is stripped)
//	# comment lines
//	                        (blank lines are ignored)
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // .env is optional
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip optional "export " prefix.
		line = strings.TrimPrefix(line, "export ")

		// Split on the first '='.
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf(".env line %d: expected KEY=VALUE, got %q", lineNum, line)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove surrounding quotes (single or double).
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		// Only set if not already defined in the real environment.
		if os.Getenv(key) == "" {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf(".env line %d: setenv %s: %w", lineNum, key, err)
			}
		}
	}

	return scanner.Err()
}
