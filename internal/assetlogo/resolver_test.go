package assetlogo

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestResolver_Enqueue_Dedup(t *testing.T) {
	r := NewResolver(nil, NewLocalStorage(t.TempDir()), NewCache())
	id := uuid.New()
	if !r.Enqueue(id) {
		t.Fatal("first enqueue should succeed")
	}
	// in-flight set prevents immediate duplicate
	if r.Enqueue(id) {
		t.Fatal("duplicate should be skipped")
	}
}

func TestResolver_Resolve_NilRepo(t *testing.T) {
	r := NewResolver(nil, NewLocalStorage(t.TempDir()), NewCache())
	id := uuid.New()
	logo, err := r.Resolve(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if logo != nil {
		t.Fatal("expected nil")
	}
}

func TestResolver_StartStop(t *testing.T) {
	r := NewResolver(nil, NewLocalStorage(t.TempDir()), NewCache())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	r.Stop()
}
