package execution

import (
	"fmt"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

var (
	ErrInvalidTransition    = domain.NewError(domain.ErrCodeOrderCreationFailed, "invalid state transition")
	ErrOrderNotFound        = domain.NewError(domain.ErrCodeOrderNotFound, "order not found")
	ErrExecutionNotFound    = domain.NewError(domain.ErrCodeOrderNotFound, "execution not found")
	ErrDuplicateClientOrder = domain.NewError(domain.ErrCodeOrderDuplicateID, "duplicate client order ID")
)

// ValidOrderTransitions defines allowed state transitions for orders
var ValidOrderTransitions = map[OrderStatus][]OrderStatus{
	OrderStatusCreated:         {OrderStatusSubmitting},
	OrderStatusSubmitting:      {OrderStatusOpen, OrderStatusRejected, OrderStatusUnknown},
	OrderStatusOpen:            {OrderStatusPartiallyFilled, OrderStatusFilled, OrderStatusCanceling, OrderStatusExpired, OrderStatusUnknown},
	OrderStatusPartiallyFilled: {OrderStatusFilled, OrderStatusCanceling, OrderStatusUnknown},
	OrderStatusCanceling:       {OrderStatusCanceled, OrderStatusUnknown},
	OrderStatusCanceled:        {},
	OrderStatusRejected:        {},
	OrderStatusExpired:         {},
	OrderStatusUnknown:         {OrderStatusCanceling, OrderStatusSubmitting},
	OrderStatusFilled:          {},
}

// CanTransition checks if a state transition is valid
func CanTransition(from, to OrderStatus) bool {
	allowed, exists := ValidOrderTransitions[from]
	if !exists {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// TransitionOrder validates and applies a state transition
func TransitionOrder(order *Order, to OrderStatus) error {
	if !CanTransition(order.Status, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, order.Status, to)
	}
	order.Status = to
	return nil
}
