package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLimiter is a fixed-window rate limiter backed by Redis: INCR a
// per-window counter key, set its TTL on the first increment of that
// window. It's implemented here (rather than in-process) because the API
// is designed to run as multiple horizontally-scaled instances — an
// in-memory counter would give every instance its own separate budget,
// which defeats the point of a shared rate limit. Every instance hitting
// the same Redis therefore shares one limit per key, the same way the WS
// broadcast fan-out (adapter/broker) shares one Redis channel.
//
// Known trade-off of a fixed window: a client can burst up to ~2x the
// limit across a window boundary (exhaust the limit at the end of one
// window, then immediately again at the start of the next). Acceptable
// for abuse protection on auth endpoints; swap for a sliding-window or
// token-bucket Lua script if stricter smoothing is needed later.
type RedisLimiter struct {
	rdb *redis.Client
}

func NewRedisLimiter(rdb *redis.Client) *RedisLimiter {
	return &RedisLimiter{rdb: rdb}
}

// Allow reports whether a request under key should proceed, given at most
// limit requests per window. When denied, retryAfter is how long until
// the window resets, suitable for a Retry-After header.
func (l *RedisLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, retryAfter time.Duration, err error) {
	count, err := l.rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, 0, err
	}

	// Only the request that creates the key sets its expiry — every
	// later INCR within the window just bumps the existing counter. If
	// the process crashed between INCR and EXPIRE the key would live
	// forever without a TTL; acceptable risk for abuse protection, not
	// a correctness-critical counter.
	if count == 1 {
		if err := l.rdb.Expire(ctx, key, window).Err(); err != nil {
			return false, 0, err
		}
	}

	if count <= int64(limit) {
		return true, 0, nil
	}

	ttl, err := l.rdb.TTL(ctx, key).Result()
	if err != nil || ttl < 0 {
		ttl = window
	}
	return false, ttl, nil
}
