package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestService_CreateLink_DeepLink(t *testing.T) {
	svc := NewService(nil, "mybot", "http://localhost:3000")
	resp, err := svc.CreateLink(context.Background(), uuid.New(), LocaleEN)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Code) != CodeLength {
		t.Fatalf("code len %d", len(resp.Code))
	}
	want := "https://t.me/mybot?start=" + resp.Code
	if resp.DeepLink != want {
		t.Fatalf("deep link %s", resp.DeepLink)
	}
	if resp.ExpiresAt.Before(time.Now()) {
		t.Fatal("expiry")
	}
}

func TestService_CreateLink_InvalidLocale(t *testing.T) {
	svc := NewService(nil, "mybot")
	_, err := svc.CreateLink(context.Background(), uuid.New(), Locale("fr"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_Status_NotConnected(t *testing.T) {
	svc := NewService(nil, "mybot")
	st, err := svc.Status(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if st.Connected {
		t.Fatal("expected disconnected")
	}
}
