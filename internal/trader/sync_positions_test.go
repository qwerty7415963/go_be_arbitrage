package trader

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// M2 scope: first sync always; watched every PositionInterval;
// unwatched every PositionColdInterval + deterministic jitter.
func TestShouldSyncPositions_Scope(t *testing.T) {
	repo := &Repository{pool: nil}
	hub := NewActivityHub()
	watched := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	other := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hub.Subscribe(watched)
	svc := NewSyncService(repo, nil, uuid.New(), DefaultSyncOptions()).
		WithPositions(HLPositionAdapter{}).
		WithActivityHub(hub)

	now := time.Now().UTC()
	// Nil positions disables.
	off := NewSyncService(repo, nil, uuid.New(), DefaultSyncOptions())
	if off.shouldSyncPositions(&SyncState{}, watched, now) {
		t.Error("nil fetcher must never sync")
	}
	// First sync always, even unwatched.
	if !svc.shouldSyncPositions(&SyncState{}, other, now) {
		t.Error("never-synced wallet must sync once")
	}
	// Watched: due after 30s, not before.
	recent := now.Add(-10 * time.Second)
	old := now.Add(-31 * time.Second)
	if svc.shouldSyncPositions(&SyncState{LastPositionsSyncAt: &recent}, watched, now) {
		t.Error("watched wallet synced 10s ago must wait")
	}
	if !svc.shouldSyncPositions(&SyncState{LastPositionsSyncAt: &old}, watched, now) {
		t.Error("watched wallet synced 31s ago must be due")
	}
	// Unwatched: cold interval (24h) + jitter.
	coldRecent := now.Add(-time.Hour)
	if svc.shouldSyncPositions(&SyncState{LastPositionsSyncAt: &coldRecent}, other, now) {
		t.Error("unwatched wallet synced 1h ago must wait")
	}
	coldOld := now.Add(-26 * time.Hour)
	if !svc.shouldSyncPositions(&SyncState{LastPositionsSyncAt: &coldOld}, other, now) {
		t.Error("unwatched wallet synced 26h ago must be due")
	}
}

func TestPositionJitter_Deterministic(t *testing.T) {
	svc := NewSyncService(&Repository{}, nil, uuid.New(), DefaultSyncOptions())
	a := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if svc.positionJitter(a) != svc.positionJitter(a) {
		t.Error("jitter must be deterministic per address")
	}
	if svc.positionJitter(a) < 0 || svc.positionJitter(a) >= svc.opts.PositionJitter {
		t.Errorf("jitter must be in [0, PositionJitter): %v", svc.positionJitter(a))
	}
}
