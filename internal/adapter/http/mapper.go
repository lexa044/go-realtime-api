package http

import (
	"github.com/lexa044/realtime-api/internal/domain"
	"github.com/lexa044/realtime-api/internal/dto/response"
)

// toOrderResponse maps a domain.Order to its wire representation. Keeping
// this mapping in one place means a change to Money's internal shape, or
// to how "never updated" is represented, only has to be taught to this
// function once — not to every handler that returns an order.
func toOrderResponse(o *domain.Order) response.OrderResponse {
	return response.OrderResponse{
		ID:         o.ID,
		CustomerID: o.CustomerID,
		Status:     o.Status.String(),
		Total:      o.Total.Amount(),
		Currency:   o.Total.Currency(),
		CreatedAt:  o.CreatedAt,
		UpdatedAt:  o.UpdatedAt,
		IsDeleted:  o.IsDeleted,
	}
}

func toOrderResponses(orders []domain.Order) []response.OrderResponse {
	out := make([]response.OrderResponse, len(orders))
	for i := range orders {
		out[i] = toOrderResponse(&orders[i])
	}
	return out
}
