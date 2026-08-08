package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/lexa044/realtime-api/internal/adapter/ws"
)

func NewRouter(orderHandler *OrderHandler, hub *ws.Hub, authMiddleware func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(authMiddleware)
		r.Post("/orders", orderHandler.Place)
		r.Get("/orders", orderHandler.List)
		r.Get("/orders/{id}", orderHandler.Get)
		r.Put("/orders/{id}", orderHandler.Update)
		r.Delete("/orders/{id}", orderHandler.Delete)
	})

	// WS upgrade also needs auth, since contextutil.UserIDKey must be set
	// beforehand.
	r.With(authMiddleware).Get("/ws", ws.Handler(hub))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return r
}
