package assetlogo

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Resolver orchestrates logo resolution with cache, storage and providers.
type Resolver struct {
	repo    *Repository
	storage LogoStorage
	cache   *Cache

	providers []Provider

	Workers int

	queue      chan uuid.UUID
	inFlight   map[uuid.UUID]bool
	inFlightMu sync.Mutex

	providerMu       sync.Mutex
	providerLocks    map[string]*sync.Mutex
	providerLast     map[string]time.Time
	providerInterval map[string]time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewResolver(repo *Repository, storage LogoStorage, cache *Cache, providers ...Provider) *Resolver {
	if cache == nil {
		cache = NewCache()
	}
	r := &Resolver{
		repo:          repo,
		storage:       storage,
		cache:         cache,
		providers:     providers,
		Workers:       2,
		queue:         make(chan uuid.UUID, 1024),
		inFlight:      make(map[uuid.UUID]bool),
		providerLocks: make(map[string]*sync.Mutex),
		providerLast:  make(map[string]time.Time),
		providerInterval: map[string]time.Duration{
			"trustwallet": 200 * time.Millisecond,
			"logodev":     500 * time.Millisecond,
		},
	}
	return r
}

func (r *Resolver) providerByName(name string) Provider {
	for _, p := range r.providers {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

func (r *Resolver) isCrypto(cls AssetClass) bool {
	return cls == AssetClassCryptoNative || cls == AssetClassCryptoToken
}

func (r *Resolver) Start(ctx context.Context) {
	if r.Workers < 1 {
		r.Workers = 2
	}
	r.ctx, r.cancel = context.WithCancel(ctx)
	for i := 0; i < r.Workers; i++ {
		r.wg.Add(1)
		go r.worker(r.ctx)
	}
}

func (r *Resolver) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

func (r *Resolver) worker(ctx context.Context) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-r.queue:
			r.processWithRetry(ctx, id)
			r.inFlightMu.Lock()
			delete(r.inFlight, id)
			r.inFlightMu.Unlock()
		}
	}
}

func (r *Resolver) processWithRetry(ctx context.Context, id uuid.UUID) {
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
		}
		_, err := r.ResolveSync(ctx, id)
		if err == nil {
			return
		}
	}
	// final: mark error if not already
	r.markError(ctx, id, fmt.Errorf("max retries exceeded"))
}

func (r *Resolver) markError(ctx context.Context, assetID uuid.UUID, err error) {
	if r.repo != nil {
		for _, name := range r.providerNames() {
			_ = r.repo.UpsertLogo(ctx, &AssetLogo{
				AssetID:    assetID,
				Provider:   name,
				Status:     LogoStatusError,
				Error:      strPtr(err.Error()),
				RetryAfter: timePtr(time.Now().Add(r.negativeTTL())),
			})
		}
	}
	if r.cache != nil {
		r.cache.Set(assetID.String(), CachedLogo{Status: string(LogoStatusError), ExpiresAt: time.Now().Add(r.negativeTTL())})
	}
}

func (r *Resolver) providerNames() []string {
	if len(r.providers) > 0 {
		names := make([]string, 0, len(r.providers))
		for _, p := range r.providers {
			names = append(names, p.Name())
		}
		return names
	}
	return []string{"trustwallet", "logodev"}
}

func (r *Resolver) readyTTL() time.Duration {
	if r.cache != nil && r.cache.ReadyTTL > 0 {
		return r.cache.ReadyTTL
	}
	return 30 * 24 * time.Hour
}

func (r *Resolver) negativeTTL() time.Duration {
	if r.cache != nil && r.cache.NegativeTTL > 0 {
		return r.cache.NegativeTTL
	}
	return 7 * 24 * time.Hour
}

func (r *Resolver) Enqueue(assetID uuid.UUID) bool {
	r.inFlightMu.Lock()
	if r.inFlight[assetID] {
		r.inFlightMu.Unlock()
		return false
	}
	r.inFlightMu.Unlock()

	if r.isFreshInDB(context.Background(), assetID) {
		return false
	}

	r.inFlightMu.Lock()
	if r.inFlight[assetID] {
		r.inFlightMu.Unlock()
		return false
	}
	r.inFlight[assetID] = true
	r.inFlightMu.Unlock()

	select {
	case r.queue <- assetID:
		return true
	default:
		r.inFlightMu.Lock()
		delete(r.inFlight, assetID)
		r.inFlightMu.Unlock()
		return false
	}
}

func (r *Resolver) isFreshInDB(ctx context.Context, assetID uuid.UUID) bool {
	if r.repo == nil {
		return false
	}
	for _, p := range r.providers {
		if logo, err := r.repo.GetLogo(ctx, assetID, p.Name()); err == nil && logo != nil {
			if logo.Status == LogoStatusFound && logo.FetchedAt != nil && time.Since(*logo.FetchedAt) < r.readyTTL() {
				return true
			}
			if (logo.Status == LogoStatusMissing || logo.Status == LogoStatusError) && logo.RetryAfter != nil && time.Now().Before(*logo.RetryAfter) {
				return true
			}
		}
	}
	if len(r.providers) == 0 {
		for _, name := range []string{"trustwallet", "logodev"} {
			if logo, err := r.repo.GetLogo(ctx, assetID, name); err == nil && logo != nil {
				if logo.Status == LogoStatusFound && logo.FetchedAt != nil && time.Since(*logo.FetchedAt) < r.readyTTL() {
					return true
				}
			}
		}
	}
	return false
}

func (r *Resolver) Resolve(ctx context.Context, assetID uuid.UUID) (*AssetLogo, error) {
	// cache lookup
	if r.cache != nil {
		if c, ok := r.cache.Get(assetID.String()); ok {
			if c.Status == string(LogoStatusFound) || c.Status == "found" || c.Status == "ready" {
				if logo := r.firstLogo(ctx, assetID); logo != nil {
					return logo, nil
				}
			}
			r.Enqueue(assetID)
			return r.firstLogo(ctx, assetID), nil
		}
	}
	// DB lookup
	logo := r.firstLogo(ctx, assetID)
	if logo != nil && r.isFreshLogo(logo) {
		r.cacheSet(logo)
		return logo, nil
	}
	r.Enqueue(assetID)
	return logo, nil
}

func (r *Resolver) ResolveSync(ctx context.Context, assetID uuid.UUID) (*AssetLogo, error) {
	if r.repo == nil {
		return nil, fmt.Errorf("repository not configured")
	}
	asset, err := r.repo.GetAsset(ctx, assetID)
	if err != nil {
		return nil, err
	}
	refs, err := r.repo.ListProviderRefs(ctx, assetID)
	if err != nil {
		refs = nil
	}
	// if no refs, nothing to do
	if len(refs) == 0 {
		return nil, fmt.Errorf("no provider refs for asset")
	}

	var lastLogo *AssetLogo
	for _, ref := range refs {
		prov := r.providerByName(ref.Provider)
		if prov == nil {
			continue
		}
		r.rateLimit(ctx, prov.Name())

		resolved, err := prov.Resolve(ctx, *asset, *ref)
		if err != nil {
			status := LogoStatusError
			if resolved.StatusCode == http.StatusNotFound {
				status = LogoStatusMissing
			}
			errStr := err.Error()
			now := time.Now()
			ra := now.Add(r.negativeTTL())
			logo := &AssetLogo{
				AssetID:    assetID,
				Provider:   prov.Name(),
				Status:     status,
				URL:        strPtrIf(resolved.URL),
				Error:      &errStr,
				RetryAfter: &ra,
			}
			if upErr := r.repo.UpsertLogo(ctx, logo); upErr != nil {
				return nil, upErr
			}
			r.cacheSetNeg(assetID, status, r.negativeTTL())
			lastLogo = logo
			if prov.Name() == "trustwallet" && r.isCrypto(asset.AssetClass) {
				break
			}
			continue
		}

		ct := resolved.ContentType
		if ct == "" {
			ct = "image/png"
		}
		key, err := r.storage.Put(ctx, assetID.String(), resolved.Bytes, ct)
		if err != nil {
			errStr := err.Error()
			now := time.Now()
			ra := now.Add(r.negativeTTL())
			logo := &AssetLogo{
				AssetID:    assetID,
				Provider:   prov.Name(),
				Status:     LogoStatusError,
				Error:      &errStr,
				RetryAfter: &ra,
			}
			_ = r.repo.UpsertLogo(ctx, logo)
			lastLogo = logo
			continue
		}

		now := time.Now()
		size := int64(len(resolved.Bytes))
		etag := resolved.ETag
		urlStr := resolved.URL
		logo := &AssetLogo{
			AssetID:     assetID,
			Provider:    prov.Name(),
			Status:      LogoStatusFound,
			URL:         &urlStr,
			StoragePath: &key,
			ContentType: &ct,
			ETag:        strPtrIf(etag),
			SizeBytes:   &size,
			FetchedAt:   &now,
		}
		if err := r.repo.UpsertLogo(ctx, logo); err != nil {
			return nil, err
		}
		r.cache.Set(assetID.String(), CachedLogo{
			LogoURL:     urlStr,
			ContentHash: sha256Hex(resolved.Bytes),
			Status:      string(LogoStatusFound),
			ExpiresAt:   now.Add(r.readyTTL()),
		})
		lastLogo = logo
		break
	}

	if lastLogo == nil {
		return nil, fmt.Errorf("resolve failed")
	}
	if lastLogo.Status != LogoStatusFound {
		return lastLogo, fmt.Errorf("logo not found")
	}
	return lastLogo, nil
}

func (r *Resolver) Prewarm(ctx context.Context) {
	if r.repo == nil {
		return
	}
	assets, err := r.repo.ListAssets(ctx)
	if err != nil {
		return
	}
	ids := make([]uuid.UUID, 0, len(assets))
	for _, a := range assets {
		ids = append(ids, a.ID)
	}
	r.ResolveMany(ctx, ids)
}

func (r *Resolver) CleanupOrphans(ctx context.Context) (int, error) {
	known := make(map[string]bool)
	if r.repo == nil {
		return 0, nil
	}
	paths, err := r.repo.ListStoragePaths(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range paths {
		known[p] = true
	}
	dir := ""
	if ls, ok := r.storage.(*LocalStorage); ok {
		dir = ls.BaseDir
	}
	if dir == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if known[full] {
			continue
		}
		if err := r.storage.Delete(ctx, full); err == nil {
			deleted++
		}
	}
	return deleted, nil
}

func (r *Resolver) ResolveMany(ctx context.Context, assetIDs []uuid.UUID) {
	seen := make(map[uuid.UUID]bool)
	for _, id := range assetIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		r.Enqueue(id)
	}
}

// LogoURL returns the serving URL for an asset logo if it is already resolved,
// without blocking or enqueuing work.
func (r *Resolver) LogoURL(ctx context.Context, assetID uuid.UUID) string {
	if r.cache != nil {
		if c, ok := r.cache.Get(assetID.String()); ok {
			status := c.Status
			if (status == string(LogoStatusFound) || status == "found" || status == "ready") && c.LogoURL != "" {
				return c.LogoURL
			}
		}
	}
	logo := r.firstLogo(ctx, assetID)
	if logo != nil && logo.Status == LogoStatusFound {
		if logo.StoragePath != nil && *logo.StoragePath != "" {
			return "/v1/assets/logos/" + assetID.String()
		}
		if logo.URL != nil && *logo.URL != "" {
			return *logo.URL
		}
	}
	return ""
}

func (r *Resolver) firstLogo(ctx context.Context, assetID uuid.UUID) *AssetLogo {
	if r.repo == nil {
		return nil
	}
	for _, p := range r.providers {
		if logo, err := r.repo.GetLogo(ctx, assetID, p.Name()); err == nil && logo != nil {
			return logo
		}
	}
	if len(r.providers) == 0 {
		for _, name := range []string{"trustwallet", "logodev"} {
			if logo, err := r.repo.GetLogo(ctx, assetID, name); err == nil && logo != nil {
				return logo
			}
		}
	}
	return nil
}

func (r *Resolver) isFreshLogo(l *AssetLogo) bool {
	if l == nil {
		return false
	}
	if l.Status == LogoStatusFound {
		if l.FetchedAt != nil && time.Since(*l.FetchedAt) < r.readyTTL() {
			return true
		}
		return false
	}
	if l.RetryAfter != nil && time.Now().Before(*l.RetryAfter) {
		return true
	}
	return false
}

func (r *Resolver) cacheSet(logo *AssetLogo) {
	if r.cache == nil || logo == nil {
		return
	}
	cl := CachedLogo{Status: string(logo.Status)}
	if logo.URL != nil {
		cl.LogoURL = *logo.URL
	}
	if logo.ETag != nil {
		cl.ContentHash = *logo.ETag
	}
	if logo.Status == LogoStatusFound {
		if logo.FetchedAt != nil {
			cl.ExpiresAt = logo.FetchedAt.Add(r.readyTTL())
		} else {
			cl.ExpiresAt = time.Now().Add(r.readyTTL())
		}
	} else {
		if logo.RetryAfter != nil {
			cl.ExpiresAt = *logo.RetryAfter
		} else {
			cl.ExpiresAt = time.Now().Add(r.negativeTTL())
		}
	}
	r.cache.Set(logo.AssetID.String(), cl)
}

func (r *Resolver) cacheSetNeg(assetID uuid.UUID, status LogoStatus, ttl time.Duration) {
	if r.cache == nil {
		return
	}
	r.cache.Set(assetID.String(), CachedLogo{Status: string(status), ExpiresAt: time.Now().Add(ttl)})
}

func (r *Resolver) rateLimit(ctx context.Context, provider string) {
	r.providerMu.Lock()
	m, ok := r.providerLocks[provider]
	if !ok {
		m = &sync.Mutex{}
		r.providerLocks[provider] = m
	}
	r.providerMu.Unlock()

	m.Lock()
	defer m.Unlock()

	interval := r.providerInterval[provider]
	if interval <= 0 {
		return
	}
	r.providerMu.Lock()
	last := r.providerLast[provider]
	r.providerMu.Unlock()
	if !last.IsZero() {
		if rem := interval - time.Since(last); rem > 0 {
			select {
			case <-time.After(rem):
			case <-ctx.Done():
				return
			}
		}
	}
	r.providerMu.Lock()
	r.providerLast[provider] = time.Now()
	r.providerMu.Unlock()
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

func strPtrIf(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

func timePtr(t time.Time) *time.Time { return &t }

// avoid unused
var _ = http.StatusOK
