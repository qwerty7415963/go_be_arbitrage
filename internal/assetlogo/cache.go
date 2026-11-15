package assetlogo

import (
	"sync"
	"time"
)

// CachedLogo is an in-memory cached representation of a logo.
type CachedLogo struct {
	LogoURL     string
	ContentHash string
	Status      string
	ExpiresAt   time.Time
}

// Cache is an in-memory cache with per-asset single-flight locking.
type Cache struct {
	mu    sync.Mutex
	items map[string]CachedLogo
	locks map[string]*sync.Mutex

	ReadyTTL    time.Duration
	NegativeTTL time.Duration
}

func NewCache() *Cache {
	return &Cache{
		items:       make(map[string]CachedLogo),
		locks:       make(map[string]*sync.Mutex),
		ReadyTTL:    30 * 24 * time.Hour,
		NegativeTTL: 7 * 24 * time.Hour,
	}
}

func (c *Cache) Get(assetID string) (CachedLogo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.items[assetID]
	if !ok {
		return CachedLogo{}, false
	}
	if !v.ExpiresAt.IsZero() && time.Now().After(v.ExpiresAt) {
		delete(c.items, assetID)
		return CachedLogo{}, false
	}
	// deep copy (value type already copied)
	out := v
	return out, true
}

func (c *Cache) Set(assetID string, logo CachedLogo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if logo.ExpiresAt.IsZero() {
		ttl := c.NegativeTTL
		if logo.Status == string(LogoStatusFound) || logo.Status == "found" || logo.Status == "ready" {
			ttl = c.ReadyTTL
		}
		logo.ExpiresAt = time.Now().Add(ttl)
	}
	c.items[assetID] = logo
}

func (c *Cache) Delete(assetID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, assetID)
}

func (c *Cache) Lock(assetID string) func() {
	c.mu.Lock()
	m, ok := c.locks[assetID]
	if !ok {
		m = &sync.Mutex{}
		c.locks[assetID] = m
	}
	c.mu.Unlock()
	m.Lock()
	return func() { m.Unlock() }
}

func (c *Cache) Do(assetID string, fn func() error) error {
	unlock := c.Lock(assetID)
	defer unlock()
	return fn()
}
