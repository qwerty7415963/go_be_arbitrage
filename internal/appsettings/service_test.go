package appsettings

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type mockRepo struct {
	val     string
	err     error
	gets    int
	sets    int
	lastSet string
}

func (m *mockRepo) Get(ctx context.Context, key string) (string, error) {
	m.gets++
	if m.err != nil {
		return "", m.err
	}
	return m.val, nil
}

func (m *mockRepo) Set(ctx context.Context, key, value string, updatedBy *uuid.UUID) error {
	m.sets++
	m.lastSet = value
	return nil
}

func (m *mockRepo) GetAll(ctx context.Context) ([]Setting, error) {
	return nil, nil
}

func TestSetMaxWalletsPerUser_Validation(t *testing.T) {
	repo := &mockRepo{val: "50"}
	svc := NewService(repo)
	ctx := context.Background()
	uid := uuid.New()
	if err := svc.SetMaxWalletsPerUser(ctx, 0, uid); err == nil {
		t.Fatal("expected error for 0")
	}
	if err := svc.SetMaxWalletsPerUser(ctx, 1001, uid); err == nil {
		t.Fatal("expected error for 1001")
	}
	if err := svc.SetMaxWalletsPerUser(ctx, 100, uid); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastSet != "100" {
		t.Fatalf("expected persisted 100, got %s", repo.lastSet)
	}
}

func TestMaxWalletsPerUser_Cache(t *testing.T) {
	repo := &mockRepo{val: "42"}
	svc := NewService(repo)
	ctx := context.Background()
	v1, err := svc.MaxWalletsPerUser(ctx)
	if err != nil || v1 != 42 {
		t.Fatalf("first: %d %v", v1, err)
	}
	repo.val = "99"
	v2, err := svc.MaxWalletsPerUser(ctx)
	if err != nil || v2 != 42 {
		t.Fatalf("second: %d %v", v2, err)
	}
	if repo.gets != 1 {
		t.Fatalf("expected 1 DB read, got %d", repo.gets)
	}
}

func TestMaxWalletsPerUser_Fallback(t *testing.T) {
	repo := &mockRepo{err: errors.New("db down")}
	svc := NewService(repo)
	ctx := context.Background()
	v, err := svc.MaxWalletsPerUser(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if v != DefaultMaxWallets {
		t.Fatalf("expected %d, got %d", DefaultMaxWallets, v)
	}
}

func TestSetMaxWalletsPerUser_CacheInvalidated(t *testing.T) {
	repo := &mockRepo{val: "50"}
	svc := NewService(repo)
	ctx := context.Background()
	uid := uuid.New()
	if _, err := svc.MaxWalletsPerUser(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetMaxWalletsPerUser(ctx, 100, uid); err != nil {
		t.Fatal(err)
	}
	repo.val = "100"
	v, err := svc.MaxWalletsPerUser(ctx)
	if err != nil || v != 100 {
		t.Fatalf("expected 100 after invalidation, got %d %v", v, err)
	}
}

func TestGetTelegramSettings(t *testing.T) {
	repo := &mockRepo{val: "77"}
	svc := NewService(repo)
	s, err := svc.GetTelegramSettings()
	if err != nil || s.MaxWalletsPerUser != 77 {
		t.Fatalf("got %+v %v", s, err)
	}
}
