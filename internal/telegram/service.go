package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type LinkResponse struct {
	Code      string    `json:"code"`
	DeepLink  string    `json:"deep_link"`
	ExpiresAt time.Time `json:"expires_at"`
}

type StatusResponse struct {
	Connected     bool   `json:"connected"`
	TGUsername    string `json:"tg_username"`
	Locale        Locale `json:"locale"`
	Status        string `json:"status"`
	GroupsEnabled int    `json:"groups_enabled"`
}

// ServiceConfig holds telegram service settings.
type ServiceConfig struct {
	BotUsername  string
	AppPublicURL string
	CodeTTL      time.Duration
}

type Service struct {
	repo         *Repository
	botUsername  string
	appPublicURL string
	codeTTL      time.Duration
}

func NewService(args ...interface{}) *Service {
	s := &Service{
		codeTTL:      10 * time.Minute,
		appPublicURL: "http://localhost:3000",
	}
	for _, a := range args {
		switch v := a.(type) {
		case *Repository:
			s.repo = v
		case *config.TelegramConfig:
			s.applyCfg(v)
		case config.TelegramConfig:
			vv := v
			s.applyCfg(&vv)
		case *config.Config:
			s.applyCfg(&v.Telegram)
		case config.Config:
			vv := v.Telegram
			s.applyCfg(&vv)
		case string:
			if s.botUsername == "" {
				s.botUsername = v
			} else if s.appPublicURL == "http://localhost:3000" {
				s.appPublicURL = v
			}
		case time.Duration:
			if v > 0 {
				s.codeTTL = v
			}
		case ServiceConfig:
			if v.BotUsername != "" {
				s.botUsername = v.BotUsername
			}
			if v.AppPublicURL != "" {
				s.appPublicURL = v.AppPublicURL
			}
			if v.CodeTTL > 0 {
				s.codeTTL = v.CodeTTL
			}
		}
	}
	if s.botUsername == "" {
		s.botUsername = "bot"
	}
	return s
}

func (s *Service) applyCfg(c *config.TelegramConfig) {
	if c == nil {
		return
	}
	if c.BotUsername != "" {
		s.botUsername = c.BotUsername
	}
	if c.AppPublicURL != "" {
		s.appPublicURL = c.AppPublicURL
	}
	if c.MaxWalletsPerUser > 0 {
		// no-op here; cap enforced elsewhere
	}
}

func (s *Service) CreateLink(ctx context.Context, userID uuid.UUID, localeArg interface{}) (*LinkResponse, error) {
	locale := ParseLocale(localeArg)
	if !locale.IsValid() {
		return nil, domain.NewError(domain.ErrCodeValidation, "invalid locale")
	}
	if s.repo != nil {
		// already connected?
		if b, err := s.repo.GetBinding(ctx, userID); err == nil && b != nil {
			return nil, domain.NewError(domain.ErrCodeConflict, "already connected")
		}
		// reuse active code
		if lc, err := s.repo.GetActiveCode(ctx, userID); err == nil && lc != nil {
			return &LinkResponse{
				Code:      lc.Code,
				DeepLink:  fmt.Sprintf("https://t.me/%s?start=%s", s.botUsername, lc.Code),
				ExpiresAt: lc.ExpiresAt,
			}, nil
		}
		_ = s.repo.DeleteUnusedCodes(ctx, userID)
	}
	code, err := GenerateCode()
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "generate code", err)
	}
	expiresAt := time.Now().Add(s.codeTTL)
	if s.repo != nil {
		lc := &LinkCode{Code: code, UserID: userID, Locale: locale, ExpiresAt: expiresAt}
		if err := s.repo.CreateCode(ctx, lc); err != nil {
			return nil, domain.WrapError(domain.ErrCodeInternal, "create code", err)
		}
	}
	return &LinkResponse{
		Code:      code,
		DeepLink:  fmt.Sprintf("https://t.me/%s?start=%s", s.botUsername, code),
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) Status(ctx context.Context, userID uuid.UUID) (*StatusResponse, error) {
	resp := &StatusResponse{Locale: LocaleVI}
	if s.repo == nil {
		return resp, nil
	}
	b, err := s.repo.GetBinding(ctx, userID)
	if err != nil {
		// not connected
		return resp, nil
	}
	if b == nil {
		return resp, nil
	}
	resp.Connected = true
	resp.Locale = b.Locale
	resp.Status = string(b.Status)
	if b.TGUsername != nil {
		resp.TGUsername = *b.TGUsername
	}
	n, _ := s.repo.CountEnabledGroups(ctx, userID)
	resp.GroupsEnabled = n
	return resp, nil
}

func (s *Service) Unlink(ctx context.Context, userID uuid.UUID) error {
	if s.repo == nil {
		return nil
	}
	return s.repo.DeleteBinding(ctx, userID)
}

// helpers for locale validation in handlers
func ParseLocale(v interface{}) Locale {
	switch val := v.(type) {
	case Locale:
		return val
	case string:
		return Locale(strings.TrimSpace(val))
	default:
		return ""
	}
}
