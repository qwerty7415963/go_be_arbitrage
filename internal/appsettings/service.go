package appsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/logger"
)

// PerpDexFetcher is the seam used to validate configured perp dex names.
type PerpDexFetcher interface {
	FetchPerpDexs(ctx context.Context) ([]string, error)
}

type Service struct {
	repo RepositoryInterface
	log  *logger.Logger

	// optional fetcher used to validate configured perp dexes.
	perpDexFetcher PerpDexFetcher

	mu       sync.Mutex
	cacheVal int
	cacheAt  time.Time
	cacheTTL time.Duration

	perpCacheVal []string
	perpCacheAt  time.Time
	perpCacheTTL time.Duration
}

func NewService(repo RepositoryInterface, loggers ...*logger.Logger) *Service {
	s := &Service{
		repo:         repo,
		cacheTTL:     5 * time.Minute,
		cacheVal:     DefaultMaxWallets,
		perpCacheTTL: 5 * time.Minute,
		perpCacheVal: []string{"xyz"},
	}
	if len(loggers) > 0 {
		s.log = loggers[0]
	}
	return s
}

// WithPerpDexFetcher attaches a fetcher used to validate configured perp dexes.
func (s *Service) WithPerpDexFetcher(f PerpDexFetcher) *Service {
	s.perpDexFetcher = f
	return s
}

// WithOnDemand is an alias for WithPerpDexFetcher, matching the trader naming.
func (s *Service) WithOnDemand(f PerpDexFetcher) *Service {
	return s.WithPerpDexFetcher(f)
}

func (s *Service) logWarn(msg string, args ...any) {
	if s.log != nil {
		s.log.Warn(msg, args...)
	} else {
		_ = msg
	}
}

func (s *Service) MaxWalletsPerUser(ctx context.Context) (int, error) {
	s.mu.Lock()
	if !s.cacheAt.IsZero() && time.Since(s.cacheAt) < s.cacheTTL {
		v := s.cacheVal
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()

	raw, err := s.repo.Get(ctx, SettingKeyTelegramMaxWallets)
	if err != nil {
		s.logWarn("appsettings: failed to read max wallets, falling back to default", "error", err)
		s.mu.Lock()
		s.cacheVal = DefaultMaxWallets
		s.cacheAt = time.Now()
		s.mu.Unlock()
		return DefaultMaxWallets, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < MinMaxWallets || v > MaxMaxWallets {
		s.logWarn("appsettings: invalid max wallets value, falling back to default", "value", raw)
		s.mu.Lock()
		s.cacheVal = DefaultMaxWallets
		s.cacheAt = time.Now()
		s.mu.Unlock()
		return DefaultMaxWallets, nil
	}
	s.mu.Lock()
	s.cacheVal = v
	s.cacheAt = time.Now()
	s.mu.Unlock()
	return v, nil
}

func (s *Service) GetTelegramSettings() (TelegramSettings, error) {
	v, _ := s.MaxWalletsPerUser(context.Background())
	return TelegramSettings{MaxWalletsPerUser: v}, nil
}

func (s *Service) SetMaxWalletsPerUser(ctx context.Context, value int, userID uuid.UUID) error {
	ts := TelegramSettings{MaxWalletsPerUser: value}
	if err := ts.Validate(); err != nil {
		return err
	}
	if err := s.repo.Set(ctx, SettingKeyTelegramMaxWallets, strconv.Itoa(value), &userID); err != nil {
		return fmt.Errorf("persist setting: %w", err)
	}
	// invalidate cache
	s.mu.Lock()
	s.cacheAt = time.Time{}
	s.mu.Unlock()
	return nil
}

func parsePerpDexes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	if strings.HasPrefix(raw, "[") {
		var arr []string
		if err := json.Unmarshal([]byte(raw), &arr); err == nil {
			out := make([]string, 0, len(arr))
			for _, d := range arr {
				d = strings.TrimSpace(d)
				if d != "" {
					out = append(out, d)
				}
			}
			if out == nil {
				out = []string{}
			}
			return out
		}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func serializePerpDexes(dexes []string) string {
	return strings.Join(dexes, ",")
}

func (s *Service) GetPerpDexes(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	if !s.perpCacheAt.IsZero() && time.Since(s.perpCacheAt) < s.perpCacheTTL {
		out := append([]string(nil), s.perpCacheVal...)
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()

	raw, err := s.repo.Get(ctx, SettingKeyPerpDexes)
	if err != nil {
		s.logWarn("appsettings: failed to read perp dexes, falling back to default", "error", err)
		s.mu.Lock()
		s.perpCacheVal = []string{"xyz"}
		s.perpCacheAt = time.Now()
		s.mu.Unlock()
		return []string{"xyz"}, nil
	}
	dexes := parsePerpDexes(raw)
	s.mu.Lock()
	s.perpCacheVal = append([]string(nil), dexes...)
	s.perpCacheAt = time.Now()
	s.mu.Unlock()
	return dexes, nil
}

func (s *Service) SetPerpDexes(ctx context.Context, dexes []string, userID uuid.UUID) error {
	ps := PerpDexSettings{Dexes: dexes}
	if err := ps.Validate(); err != nil {
		return err
	}
	// known validation
	if s.perpDexFetcher != nil {
		known, err := s.perpDexFetcher.FetchPerpDexs(ctx)
		if err != nil {
			return fmt.Errorf("fetch perp dexes: %w", err)
		}
		knownSet := make(map[string]struct{}, len(known))
		for _, k := range known {
			knownSet[strings.TrimSpace(k)] = struct{}{}
		}
		for _, d := range dexes {
			if _, ok := knownSet[strings.TrimSpace(d)]; !ok {
				return domain.NewError(domain.ErrCodeValidation, "unknown perp dex: "+d)
			}
		}
	}
	if err := s.repo.Set(ctx, SettingKeyPerpDexes, serializePerpDexes(dexes), &userID); err != nil {
		return fmt.Errorf("persist setting: %w", err)
	}
	s.mu.Lock()
	s.perpCacheAt = time.Time{}
	s.mu.Unlock()
	return nil
}
