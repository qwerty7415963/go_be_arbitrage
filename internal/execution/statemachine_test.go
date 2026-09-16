package execution

import (
	"testing"
)

func TestOrderState_CreatedToSubmitting(t *testing.T) {
	order := &Order{Status: OrderStatusCreated}
	if err := TransitionOrder(order, OrderStatusSubmitting); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != OrderStatusSubmitting {
		t.Errorf("expected SUBMITTING, got %s", order.Status)
	}
}

func TestOrderState_SubmittingToOpen(t *testing.T) {
	order := &Order{Status: OrderStatusSubmitting}
	if err := TransitionOrder(order, OrderStatusOpen); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != OrderStatusOpen {
		t.Errorf("expected OPEN, got %s", order.Status)
	}
}

func TestOrderState_SubmittingToRejected(t *testing.T) {
	order := &Order{Status: OrderStatusSubmitting}
	if err := TransitionOrder(order, OrderStatusRejected); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != OrderStatusRejected {
		t.Errorf("expected REJECTED, got %s", order.Status)
	}
}

func TestOrderState_OpenToFilled(t *testing.T) {
	order := &Order{Status: OrderStatusOpen}
	if err := TransitionOrder(order, OrderStatusFilled); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != OrderStatusFilled {
		t.Errorf("expected FILLED, got %s", order.Status)
	}
}

func TestOrderState_OpenToPartiallyFilled(t *testing.T) {
	order := &Order{Status: OrderStatusOpen}
	if err := TransitionOrder(order, OrderStatusPartiallyFilled); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != OrderStatusPartiallyFilled {
		t.Errorf("expected PARTIALLY_FILLED, got %s", order.Status)
	}
}

func TestOrderState_PartiallyFilledToFilled(t *testing.T) {
	order := &Order{Status: OrderStatusPartiallyFilled}
	if err := TransitionOrder(order, OrderStatusFilled); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != OrderStatusFilled {
		t.Errorf("expected FILLED, got %s", order.Status)
	}
}

func TestOrderState_OpenToCanceling(t *testing.T) {
	order := &Order{Status: OrderStatusOpen}
	if err := TransitionOrder(order, OrderStatusCanceling); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != OrderStatusCanceling {
		t.Errorf("expected CANCELING, got %s", order.Status)
	}
}

func TestOrderState_CancelingToCanceled(t *testing.T) {
	order := &Order{Status: OrderStatusCanceling}
	if err := TransitionOrder(order, OrderStatusCanceled); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != OrderStatusCanceled {
		t.Errorf("expected CANCELED, got %s", order.Status)
	}
}

func TestOrderState_OpenToExpired(t *testing.T) {
	order := &Order{Status: OrderStatusOpen}
	if err := TransitionOrder(order, OrderStatusExpired); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != OrderStatusExpired {
		t.Errorf("expected EXPIRED, got %s", order.Status)
	}
}

func TestOrderState_OpenToUnknown(t *testing.T) {
	order := &Order{Status: OrderStatusOpen}
	if err := TransitionOrder(order, OrderStatusUnknown); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != OrderStatusUnknown {
		t.Errorf("expected UNKNOWN, got %s", order.Status)
	}
}

func TestOrderState_InvalidTransition_CreatedToFilled(t *testing.T) {
	order := &Order{Status: OrderStatusCreated}
	err := TransitionOrder(order, OrderStatusFilled)
	if err == nil {
		t.Error("expected error for invalid transition CREATED -> FILLED")
	}
}

func TestOrderState_InvalidTransition_CanceledToOpen(t *testing.T) {
	order := &Order{Status: OrderStatusCanceled}
	err := TransitionOrder(order, OrderStatusOpen)
	if err == nil {
		t.Error("expected error for invalid transition CANCELED -> OPEN")
	}
}

func TestOrderState_InvalidTransition_RejectedToOpen(t *testing.T) {
	order := &Order{Status: OrderStatusRejected}
	err := TransitionOrder(order, OrderStatusOpen)
	if err == nil {
		t.Error("expected error for invalid transition REJECTED -> OPEN")
	}
}

func TestCanTransition_Valid(t *testing.T) {
	tests := []struct {
		from OrderStatus
		to   OrderStatus
		want bool
	}{
		{OrderStatusCreated, OrderStatusSubmitting, true},
		{OrderStatusSubmitting, OrderStatusOpen, true},
		{OrderStatusOpen, OrderStatusFilled, true},
		{OrderStatusCreated, OrderStatusFilled, false},
		{OrderStatusCanceled, OrderStatusOpen, false},
		{OrderStatusFilled, OrderStatusOpen, false},
		{OrderStatusUnknown, OrderStatusCanceling, true},
		{OrderStatusUnknown, OrderStatusSubmitting, true},
	}

	for _, tt := range tests {
		got := CanTransition(tt.from, tt.to)
		if got != tt.want {
			t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestPolicySelector_Parallel(t *testing.T) {
	sel := NewPolicySelector()
	intent := &ExecutionIntent{
		Legs: []IntentLeg{
			{Side: SideBuy},
		},
	}
	if sel.Select(intent) != PolicyParallel {
		t.Error("expected PARALLEL for single leg")
	}
}

func TestPolicySelector_Sequential(t *testing.T) {
	sel := NewPolicySelector()
	intent := &ExecutionIntent{
		Legs: []IntentLeg{
			{Side: SideBuy},
			{Side: SideSell},
		},
	}
	if sel.Select(intent) != PolicySequential {
		t.Error("expected SEQUENTIAL for multi-leg")
	}
}
