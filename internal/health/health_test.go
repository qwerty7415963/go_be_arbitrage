package health

import (
	"context"
	"errors"
	"testing"
)

type mockChecker struct {
	err error
}

func (m *mockChecker) Ping(ctx context.Context) error {
	return m.err
}

func TestNewHandler(t *testing.T) {
	h := NewHandler()
	if h == nil {
		t.Error("expected handler to be non-nil")
	}
}

func TestRegister(t *testing.T) {
	h := NewHandler()
	checker := &mockChecker{}
	h.Register("test", checker)
}

func TestHealth_AllHealthy(t *testing.T) {
	h := NewHandler()
	h.Register("service1", &mockChecker{})
	h.Register("service2", &mockChecker{})

	if len(h.checkers) != 2 {
		t.Errorf("expected 2 checkers, got %d", len(h.checkers))
	}
}

func TestHealth_OneUnhealthy(t *testing.T) {
	h := NewHandler()
	h.Register("service1", &mockChecker{})
	h.Register("service2", &mockChecker{err: errors.New("unhealthy")})

	if len(h.checkers) != 2 {
		t.Errorf("expected 2 checkers, got %d", len(h.checkers))
	}
}
