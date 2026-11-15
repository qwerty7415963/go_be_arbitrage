package assetlogo

import (
	"testing"
	"time"
)

func TestCache_HitDeepCopyAndTTL(t *testing.T) {
	c := NewCache()
	c.Set("a", CachedLogo{LogoURL: "u", Status: "found"})
	got, ok := c.Get("a")
	if !ok {
		t.Fatal("expected hit")
	}
	got.LogoURL = "mutated"
	got2, _ := c.Get("a")
	if got2.LogoURL != "u" {
		t.Fatal("deep copy failed")
	}
	if got2.ExpiresAt.IsZero() {
		t.Fatal("expires not set")
	}
	// expired
	c2 := NewCache()
	c2.Set("b", CachedLogo{Status: "found", ExpiresAt: time.Now().Add(-time.Second)})
	if _, ok := c2.Get("b"); ok {
		t.Fatal("expected expired miss")
	}
}

func TestCache_NegativeTTL(t *testing.T) {
	c := NewCache()
	c.Set("a", CachedLogo{Status: "missing"})
	got, ok := c.Get("a")
	if !ok {
		t.Fatal("expected hit")
	}
	if time.Until(got.ExpiresAt) > 8*24*time.Hour {
		t.Fatal("negative ttl too long")
	}
}

func TestCache_SingleFlightLock(t *testing.T) {
	c := NewCache()
	unlock := c.Lock("x")
	done := make(chan struct{})
	go func() {
		unlock2 := c.Lock("x")
		close(done)
		unlock2()
	}()
	select {
	case <-done:
		t.Fatal("should block")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("should unblock")
	}
}
