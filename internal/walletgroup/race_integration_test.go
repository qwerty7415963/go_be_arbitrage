//go:build integration

package walletgroup

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// collect runs fn in n goroutines and returns all non-nil errors.
func collect(t *testing.T, n int, fn func(i int) error) []error {
	t.Helper()
	var mu sync.Mutex
	var errs []error
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := fn(i); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	return errs
}

func assertCleanErrors(t *testing.T, errs []error) {
	t.Helper()
	for _, err := range errs {
		var appErr *domain.AppError
		if !errors.As(err, &appErr) {
			t.Errorf("raw (non-AppError) error under race: %T %v", err, err)
		}
	}
}

func memberCount(t *testing.T, f *fixture, groupID uuid.UUID) int64 {
	t.Helper()
	_, total, err := f.repo.ListMembers(context.Background(), groupID, "", 200, 0)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	return total
}

// HARD-01: concurrent identical adds → exactly one membership (TEST-07).
func TestRace_ConcurrentIdenticalAdd_NoDuplicate(t *testing.T) {
	f := setupWalletGroupFixture(t)
	ctx := context.Background()
	g := f.createGroup(t, f.userA, "HARD-01 Group")
	addr := f.trackAddr("0x0000000000000000000000000000000000aa0001")

	errs := collect(t, 8, func(i int) error {
		_, err := f.svc.AddWallets(ctx, f.userA, g.ID, &WalletsRequest{Wallets: []string{addr}})
		return err
	})
	if len(errs) != 0 {
		t.Fatalf("expected all idempotent adds to succeed, got %v", errs)
	}
	if n := memberCount(t, f, g.ID); n != 1 {
		t.Errorf("expected exactly 1 membership, got %d", n)
	}
}

// HARD-02: concurrent add + remove → consistent state, no duplicates.
func TestRace_ConcurrentAddRemove_Consistent(t *testing.T) {
	f := setupWalletGroupFixture(t)
	ctx := context.Background()
	g := f.createGroup(t, f.userA, "HARD-02 Group")
	addr := f.trackAddr("0x0000000000000000000000000000000000aa0002")

	errs := collect(t, 8, func(i int) error {
		req := &WalletsRequest{Wallets: []string{addr}}
		if i%2 == 0 {
			_, err := f.svc.AddWallets(ctx, f.userA, g.ID, req)
			return err
		}
		_, err := f.svc.RemoveWallets(ctx, f.userA, g.ID, req)
		return err
	})
	assertCleanErrors(t, errs)

	n := memberCount(t, f, g.ID)
	if n != 0 && n != 1 {
		t.Errorf("inconsistent membership state: %d rows", n)
	}
}

// HARD-03: concurrent PATCH + DELETE → one wins, loser gets a clean
// not-found error (GROUP-001), never a raw failure.
func TestRace_ConcurrentPatchDelete_CleanConflict(t *testing.T) {
	f := setupWalletGroupFixture(t)
	ctx := context.Background()
	g := f.createGroup(t, f.userA, "HARD-03 Group")
	name := "HARD-03 Renamed"

	errs := collect(t, 7, func(i int) error {
		if i < 6 {
			_, err := f.svc.UpdateGroup(ctx, f.userA, g.ID, &UpdateGroupRequest{Name: &name})
			return err
		}
		return f.svc.DeleteGroup(ctx, f.userA, g.ID)
	})
	assertCleanErrors(t, errs)
	for _, err := range errs {
		var appErr *domain.AppError
		_ = errors.As(err, &appErr)
		if appErr.Code != domain.ErrCodeGroupNotFound {
			t.Errorf("loser must see GROUP-001, got %s", appErr.Code)
		}
	}

	// Final state is one of the two legal outcomes.
	got, err := f.svc.GetGroup(ctx, f.userA, g.ID)
	if err != nil {
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeGroupNotFound {
			t.Fatalf("unexpected final error: %v", err)
		}
		return // deleted won
	}
	if got.Name != name {
		t.Errorf("patch won but name is %q", got.Name)
	}
}
