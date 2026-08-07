package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/lexa044/realtime-api/internal/domain"
	"github.com/lexa044/realtime-api/internal/usecase"
)

type OrderHandler struct {
	svc usecase.OrderService
}

func NewOrderHandler(svc usecase.OrderService) *OrderHandler {
	return &OrderHandler{svc: svc}
}

type placeOrderRequest struct {
	CustomerID string  `json:"customer_id"`
	Total      float64 `json:"total"`
}

func (h *OrderHandler) Place(w http.ResponseWriter, r *http.Request) {
	var req placeOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	order, err := h.svc.PlaceOrder(r.Context(), req.CustomerID, req.Total)
	if err != nil {
		http.Error(w, "could not place order", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(order)
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
	json.NewEncoder(w).Encode(order)
}

type listOrdersResponse struct {
	Data       []domain.Order `json:"data"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalCount int            `json:"total_count"`
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
	json.NewEncoder(w).Encode(listOrdersResponse{
		Data:       result.Orders,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalCount: result.TotalCount,
	})
}

type updateOrderRequest struct {
	CustomerID string  `json:"customer_id"`
	Status     string  `json:"status"`
	Total      float64 `json:"total"`
}

func (h *OrderHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req updateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	order, err := h.svc.UpdateOrder(r.Context(), id, req.CustomerID, req.Status, req.Total)
	if err != nil {
		if errors.Is(err, domain.ErrOrderNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "could not update order", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(order)
}

// Delete performs a logical delete — the row stays in MSSQL with
// IsDeleted=1, it just stops showing up in GetByID/List.
func (h *OrderHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteOrder(r.Context(), id); err != nil {
		if errors.Is(err, domain.ErrOrderNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "could not delete order", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
