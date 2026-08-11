package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

type fakeRateLimiter struct {
	allowFn func(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error)
}

func (f *fakeRateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	return f.allowFn(ctx, key, limit, window)
}

// withClientIP wraps a handler with the same middleware.ClientIPFromRemoteAddr
// router.go installs globally in production — RateLimitMiddleware reads the
// IP from context via middleware.GetClientIP, so it depends on one of chi's
// ClientIPFrom* middlewares having already run.
func withClientIP(h http.Handler) http.Handler {
	return middleware.ClientIPFromRemoteAddr(h)
}

func TestRateLimitMiddleware_Allows(t *testing.T) {
	limiter := &fakeRateLimiter{
		allowFn: func(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
			return true, 0, nil
		},
	}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "1.2.3.4:5555"
	rec := httptest.NewRecorder()

	withClientIP(RateLimitMiddleware(limiter, "test", 5, time.Minute)(next)).ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected the next handler to be called when allowed")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRateLimitMiddleware_DeniesWithRetryAfter(t *testing.T) {
	limiter := &fakeRateLimiter{
		allowFn: func(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
			return false, 30 * time.Second, nil
		},
	}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "1.2.3.4:5555"
	rec := httptest.NewRecorder()

	withClientIP(RateLimitMiddleware(limiter, "test", 5, time.Minute)(next)).ServeHTTP(rec, req)

	if called {
		t.Fatal("expected the next handler NOT to be called when denied")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Fatalf("expected Retry-After: 30, got %q", got)
	}
}

func TestRateLimitMiddleware_FailsOpenOnLimiterError(t *testing.T) {
	limiter := &fakeRateLimiter{
		allowFn: func(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
			return false, 0, errors.New("redis down")
		},
	}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "1.2.3.4:5555"
	rec := httptest.NewRecorder()

	withClientIP(RateLimitMiddleware(limiter, "test", 5, time.Minute)(next)).ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected the next handler to be called when the limiter backend errors (fail open)")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// TestRateLimitMiddleware_KeyIncludesResolvedClientIP verifies the real
// wiring end-to-end: given the same middleware.ClientIPFromRemoteAddr
// router.go installs, RateLimitMiddleware's key contains the IP that
// middleware actually resolved — not a raw, unparsed "ip:port" RemoteAddr.
func TestRateLimitMiddleware_KeyIncludesResolvedClientIP(t *testing.T) {
	var gotKey string
	limiter := &fakeRateLimiter{
		allowFn: func(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
			gotKey = key
			return true, 0, nil
		},
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "203.0.113.5:54321"
	rec := httptest.NewRecorder()

	withClientIP(RateLimitMiddleware(limiter, "login", 5, time.Minute)(next)).ServeHTTP(rec, req)

	const want = "ratelimit:login:203.0.113.5"
	if gotKey != want {
		t.Fatalf("expected key %q, got %q", want, gotKey)
	}
}
