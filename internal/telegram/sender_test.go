package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSender_ClassifyErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Forbidden: bot was blocked by the user"}`))
	}))
	defer ts.Close()
	s := NewSender("token", ts.URL)
	err := s.SendMessage(context.Background(), 1, "hi")
	if err == nil || !IsPermanent(err) {
		t.Fatalf("expected permanent 403, got %v", err)
	}

	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: chat not found"}`))
	}))
	defer ts2.Close()
	s2 := NewSender("token", ts2.URL)
	err2 := s2.SendMessage(2, "hi")
	if err2 == nil || !IsPermanent(err2) {
		t.Fatalf("expected permanent chat not found, got %v", err2)
	}

	ts3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"description":"server error"}`))
	}))
	defer ts3.Close()
	s3 := NewSender("token", ts3.URL)
	err3 := s3.SendMessage(3, "hi")
	if err3 == nil || IsPermanent(err3) {
		t.Fatalf("expected retryable, got %v", err3)
	}
}
