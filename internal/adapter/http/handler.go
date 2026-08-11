package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/lexa044/realtime-api/internal/contextutil"
	"github.com/lexa044/realtime-api/internal/domain"
	"github.com/lexa044/realtime-api/internal/dto/request"
	"github.com/lexa044/realtime-api/internal/dto/response"
	"github.com/lexa044/realtime-api/internal/usecase"
)

// defaultCurrency is applied when a request omits the currency field. This
// is an API-layer convenience, not a domain rule — domain.NewMoney always
// requires an explicit currency; the handler just chooses what to pass it
// when the caller didn't specify one.
const defaultCurrency = "USD"

type OrderHandler struct {
	svc usecase.OrderService
}

func NewOrderHandler(svc usecase.OrderService) *OrderHandler {
	return &OrderHandler{svc: svc}
}

// actorFromContext reads the authenticated user's ID, set by AuthMiddleware
// for every request that reaches these handlers (all of them require
// auth). Every order write threads this through as CreatedBy/UpdatedBy.
func actorFromContext(r *http.Request) string {
	userID, _ := r.Context().Value(contextutil.UserIDKey).(string)
	return userID
}

func (h *OrderHandler) Place(w http.ResponseWriter, r *http.Request) {
	var req request.PlaceOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	currency := req.Currency
	if currency == "" {
		currency = defaultCurrency
	}
	total, err := domain.NewMoney(req.Total, currency)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	order, err := h.svc.PlaceOrder(r.Context(), actorFromContext(r), req.CustomerID, total)
	if err != nil {
		http.Error(w, "could not place order", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(toOrderResponse(order))
}

func (h *OrderHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	order, err := h.svc.GetOrder(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrOrderNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "could not get order", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toOrderResponse(order))
}

// List handles GET /orders?customer_id=&page=&page_size=. Paging inputs
// are parsed leniently here (invalid/missing values fall through to
// usecase-level clamping) rather than rejected outright, since a malformed
// page number isn't worth a 400 when "just give me page 1" is a reasonable
// interpretation.
func (h *OrderHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))

	result, err := h.svc.ListOrders(r.Context(), usecase.ListOrdersParams{
		CustomerID: q.Get("customer_id"),
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		http.Error(w, "could not list orders", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response.ListOrdersResponse{
		Data:       toOrderResponses(result.Orders),
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalCount: result.TotalCount,
	})
}

// Update is a full replace (PUT semantics): customer_id, status, and total
// must all be present in the body, or they'll be overwritten with zero
// values. Status and currency are validated against the domain's closed
// sets before anything reaches the usecase layer — an invalid value here
// is a 400, not a 500.
func (h *OrderHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req request.UpdateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	status, err := domain.ParseOrderStatus(req.Status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	currency := req.Currency
	if currency == "" {
		currency = defaultCurrency
	}
	total, err := domain.NewMoney(req.Total, currency)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	order, err := h.svc.UpdateOrder(r.Context(), actorFromContext(r), id, req.CustomerID, status, total)
	if err != nil {
		if errors.Is(err, domain.ErrOrderNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "could not update order", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toOrderResponse(order))
}

// Delete performs a logical delete — the row stays in MSSQL with
// IsDeleted=1, it just stops showing up in GetByID/List.
func (h *OrderHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteOrder(r.Context(), actorFromContext(r), id); err != nil {
		if errors.Is(err, domain.ErrOrderNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "could not delete order", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
