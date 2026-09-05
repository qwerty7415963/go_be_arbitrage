package logger

import (
	"bytes"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name   string
		level  string
		format string
	}{
		{"json info", "info", "json"},
		{"text debug", "debug", "text"},
		{"warn json", "warn", "json"},
		{"error text", "error", "text"},
		{"invalid level defaults to info", "invalid", "json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := New(tt.level, tt.format)
			if l == nil {
				t.Error("expected logger to be non-nil")
			}
		})
	}
}

func TestNewWithWriter(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter("info", "json", &buf)
	if l == nil {
		t.Error("expected logger to be non-nil")
	}
}

func TestWithFields(t *testing.T) {
	l := New("info", "json")
	l2 := l.WithFields(map[string]interface{}{
		"key": "value",
	})
	if l2 == nil {
		t.Error("expected logger to be non-nil")
	}
}

func TestWithError(t *testing.T) {
	l := New("info", "json")
	l2 := l.WithError(nil)
	if l2 == nil {
		t.Error("expected logger to be non-nil")
	}
}

func TestWithRequestID(t *testing.T) {
	l := New("info", "json")
	l2 := l.WithRequestID("test-request-id")
	if l2 == nil {
		t.Error("expected logger to be non-nil")
	}
}

func TestWithModule(t *testing.T) {
	l := New("info", "json")
	l2 := l.WithModule("test-module")
	if l2 == nil {
		t.Error("expected logger to be non-nil")
	}
}
