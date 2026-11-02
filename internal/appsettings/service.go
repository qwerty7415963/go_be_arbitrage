package appsettings

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/logger"
)

type Service struct {
	repo RepositoryInterface
	log  *logger.Logger

	mu       sync.Mutex
	cacheVal int
	cacheAt  time.Time
	cacheTTL time.Duration
}

func NewService(repo RepositoryInterface, loggers ...*logger.Logger) *Service {
	s := &Service{
		repo:     repo,
		cacheTTL: 5 * time.Minute,
		cacheVal: DefaultMaxWallets,
	}
	if len(loggers) > 0 {
		s.log = loggers[0]
	}
	return s
}

func (s *Service) logWarn(msg string, args ...any) {
	if s.log != nil {
		s.log.Warn(msg, args...)
	} else {
		// fallback to default slog? keep silent in tests
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
