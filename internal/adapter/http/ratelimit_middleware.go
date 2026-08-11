package http

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// RateLimiter is the minimal interface this middleware needs from a rate
// limiting backend. Implemented by adapter/ratelimit.RedisLimiter; kept
// as a small interface here (rather than importing that package's
// concrete type) so this middleware is unit-testable without Redis.
type RateLimiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, retryAfter time.Duration, err error)
}

// RateLimitMiddleware rejects with 429 once more than limit requests from
// the same client IP hit bucket within window. bucket namespaces the
// counter (e.g. "login" vs "auth") so different routes don't share a
// budget unless intentionally configured to.
//
// The client IP comes from middleware.GetClientIP, which reads whatever
// router.go's chosen middleware.ClientIPFrom* installed into the request
// context — never r.RemoteAddr directly, since that's the load balancer
// or reverse proxy's address whenever one sits in front of this API, not
// the real client's. See router.go's comment for which ClientIPFrom*
// middleware is active and why; that choice, not anything here, is what
// determines whether this rate limiter is spoofable.
//
// Fails OPEN if the limiter backend errors (e.g. Redis unreachable):
// availability of the auth endpoints matters more than rate limiting
// continuing to function during a Redis outage. The failure is logged so
// it's visible, not silent.
func RateLimitMiddleware(limiter RateLimiter, bucket string, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := middleware.GetClientIP(r.Context())
			key := "ratelimit:" + bucket + ":" + ip

			allowed, retryAfter, err := limiter.Allow(r.Context(), key, limit, window)
			if err != nil {
				log.Printf("rate limiter error on bucket %q (failing open): %v", bucket, err)
				next.ServeHTTP(w, r)
				return
			}
			if !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
