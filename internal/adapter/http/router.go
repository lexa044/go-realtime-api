package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/lexa044/realtime-api/internal/adapter/ws"
)

func NewRouter(
	orderHandler *OrderHandler,
	authHandler *AuthHandler,
	userHandler *UserHandler,
	hub *ws.Hub,
	authMiddleware func(http.Handler) http.Handler,
	loginRateLimit func(http.Handler) http.Handler,
	authRateLimit func(http.Handler) http.Handler,
) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	// middleware.RealIP is deprecated as of chi v5.3.0 — it's vulnerable
	// to IP spoofing (GHSA-3fxj-6jh8-hvhx, rated Critical; also
	// GHSA-rjr7-jggh-pgcp, GHSA-9g5q-2w5x-hmxf) because it trusts
	// True-Client-IP/X-Real-IP/X-Forwarded-For unconditionally, whether
	// or not this deployment actually sits behind something that sets
	// them. chi replaced it with four explicit ClientIPFrom* middlewares
	// — there's deliberately no safe default, you must pick exactly one
	// based on your topology:
	//
	//   ClientIPFromRemoteAddr        — directly on the public internet, no proxy
	//   ClientIPFromHeader("X-Real-IP") — proxy sets ONE header and overwrites it every request
	//   ClientIPFromXFF("10.0.0.0/8")   — proxies with enumerable CIDRs (e.g. your VPC, Cloudflare's list)
	//   ClientIPFromXFFTrustedProxies(2) — known number of proxies, dynamic IPs
	//
	// ClientIPFromRemoteAddr is used here because this project doesn't
	// assume any particular reverse proxy is in front of it. If you
	// deploy behind one, swap this for whichever of the other three
	// matches your setup — using ClientIPFromRemoteAddr behind a proxy
	// would rate-limit the proxy's IP instead of each real client's,
	// while using an XFF-trusting middleware with nothing in front to
	// enforce it would let any client set their own rate-limit identity.
	// RateLimitMiddleware reads whatever this sets via
	// middleware.GetClientIP — swapping this one line is the only change
	// needed to retarget the whole rate limiter.
	r.Use(middleware.ClientIPFromRemoteAddr)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Login/refresh/logout are reachable WITHOUT a bearer token — they're
	// how a client gets one in the first place — which is exactly why
	// they're rate-limited: nothing else stands between these routes and
	// the internet. login gets the stricter budget (password guessing);
	// refresh/logout share a looser one (their tokens are already
	// high-entropy and unguessable, so the risk there is abuse/DoS, not
	// brute force).
	r.Route("/auth", func(r chi.Router) {
		r.With(loginRateLimit).Post("/login", authHandler.Login)
		r.With(authRateLimit).Post("/refresh", authHandler.Refresh)
		r.With(authRateLimit).Post("/logout", authHandler.Logout)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(authMiddleware)
		r.Post("/orders", orderHandler.Place)
		r.Get("/orders", orderHandler.List)
		r.Get("/orders/{id}", orderHandler.Get)
		r.Put("/orders/{id}", orderHandler.Update)
		r.Delete("/orders/{id}", orderHandler.Delete)
		r.Get("/users/me", userHandler.Me)
	})

	// WS upgrade also needs auth, since contextutil.UserIDKey must be set
	// beforehand.
	r.With(authMiddleware).Get("/ws", ws.Handler(hub))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return r
}
