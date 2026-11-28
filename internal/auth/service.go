package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/mail"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	Role     string `json:"role"`
	Type     string `json:"typ"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

type Service struct {
	config    *config.AuthConfig
	repo      *Repository
	captcha   *TurnstileVerifier
	auditRepo *storage.AuditRepository
}

func (s *Service) WithAuditRepo(repo *storage.AuditRepository) *Service {
	s.auditRepo = repo
	return s
}

func NewService(cfg *config.AuthConfig, repo *Repository) *Service {
	s := &Service{config: cfg, repo: repo}
	s.captcha = NewTurnstileVerifier(cfg.TurnstileSecretKey, cfg.TurnstileExpectedHostnames, 3*time.Second, cfg.CaptchaEnabled, cfg.Mode)
	return s
}

func (s *Service) Register(ctx context.Context, req *RegisterRequest) (*AuthResponse, error) {
	if _, err := mail.ParseAddress(req.Email); err != nil {
		return nil, domain.NewError(domain.ErrCodeValidation, "invalid email format")
	}
	if len(req.Password) < 12 || len(req.Password) > 128 {
		return nil, domain.NewError(domain.ErrCodeValidation, "password must be 12-128 characters")
	}
	// CAPTCHA
	if s.captcha != nil {
		if err := s.captcha.Verify(ctx, req.CaptchaToken); err != nil {
			return nil, domain.NewError(domain.ErrCodeAuthCaptchaFailed, "captcha verification failed")
		}
	}

	exists, err := s.repo.ExistsByEmail(ctx, req.Email)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to check email", err)
	}
	if exists {
		return nil, domain.NewError(domain.ErrCodeConflict, "email already registered")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to hash password", err)
	}

	user := &User{
		ID:           uuid.New(),
		Email:        req.Email,
		PasswordHash: string(hash),
		Role:         "user",
		Status:       "ACTIVE",
	}

	if err := s.repo.CreateUser(ctx, user); err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to create user", err)
	}

	if s.auditRepo != nil {
		actorID := user.ID.String()
		entityType := "user"
		_ = s.auditRepo.CreateAuditEvent(ctx, &storage.AuditEvent{
			ActorType:  "user",
			ActorID:    &actorID,
			Action:     "auth.register",
			EntityType: &entityType,
			EntityID:   &actorID,
			OccurredAt: time.Now(),
		})
	}

	return s.generateAuthResponse(ctx, user)
}

func (s *Service) Login(ctx context.Context, req *LoginRequest) (*AuthResponse, error) {
	user, err := s.repo.GetUserByEmail(ctx, req.Email)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeAuthInvalidCredentials, "invalid email or password")
	}

	if user.Status == "DISABLED" {
		return nil, domain.NewError(domain.ErrCodeAuthDisabled, "account is disabled")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, domain.NewError(domain.ErrCodeAuthInvalidCredentials, "invalid email or password")
	}

	_ = s.repo.UpdateLastLogin(ctx, user.ID)

	if s.auditRepo != nil {
		actorID := user.ID.String()
		entityType := "user"
		_ = s.auditRepo.CreateAuditEvent(ctx, &storage.AuditEvent{
			ActorType:  "user",
			ActorID:    &actorID,
			Action:     "auth.login",
			EntityType: &entityType,
			EntityID:   &actorID,
			OccurredAt: time.Now(),
		})
	}

	return s.generateAuthResponse(ctx, user)
}

func (s *Service) Refresh(ctx context.Context, refreshTokenStr string) (*AuthResponse, error) {
	if refreshTokenStr == "" {
		return nil, domain.NewError(domain.ErrCodeAuthTokenInvalid, "invalid refresh token")
	}
	tokenHash := hashToken(refreshTokenStr)

	old, err := s.repo.GetRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeAuthTokenInvalid, "invalid refresh token")
	}

	if old.Revoked {
		_ = s.repo.RevokeRefreshTokenFamily(ctx, old.FamilyID)
		return nil, domain.NewError(domain.ErrCodeAuthRefreshRevoked, "refresh token reused")
	}

	if time.Now().After(old.ExpiresAt) {
		return nil, domain.NewError(domain.ErrCodeAuthTokenExpired, "refresh token expired")
	}

	user, err := s.repo.GetUserByID(ctx, old.UserID)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeNotFound, "user not found")
	}

	if user.Status == "DISABLED" {
		return nil, domain.NewError(domain.ErrCodeAuthDisabled, "account is disabled")
	}

	newRaw, err := generateRefreshToken()
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to generate refresh token", err)
	}
	newToken := &RefreshToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: hashToken(newRaw),
		FamilyID:  old.FamilyID,
		ExpiresAt: time.Now().Add(s.config.RefreshExpiration),
		Revoked:   false,
	}

	if err := s.repo.RotateRefreshToken(ctx, tokenHash, newToken); err != nil {
		_ = s.repo.RevokeRefreshTokenFamily(ctx, old.FamilyID)
		return nil, domain.NewError(domain.ErrCodeAuthRefreshRevoked, "refresh token reused")
	}

	tenantID := ""
	if user.TenantID != nil {
		tenantID = user.TenantID.String()
	}
	accessToken, err := s.generateToken(user.ID.String(), tenantID, user.Role, s.config.JWTExpiration)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to generate access token", err)
	}

	return &AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: newRaw,
		ExpiresAt:    time.Now().Add(s.config.JWTExpiration).Unix(),
		User: &UserResponse{
			ID:         user.ID.String(),
			Email:      user.Email,
			Role:       user.Role,
			Status:     user.Status,
			AuthMethod: user.AuthMethod,
			CreatedAt:  user.CreatedAt.Format(time.RFC3339),
		},
	}, nil
}

func (s *Service) Logout(ctx context.Context, v interface{}) error {
	switch val := v.(type) {
	case uuid.UUID:
		return s.repo.RevokeAllUserRefreshTokens(ctx, val)
	case string:
		if val == "" {
			return nil
		}
		tok, err := s.repo.GetRefreshTokenByHash(ctx, hashToken(val))
		if err != nil {
			return nil
		}
		return s.repo.RevokeRefreshTokenFamily(ctx, tok.FamilyID)
	default:
		return nil
	}
}

func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, oldPassword, newPassword string) error {
	if len(newPassword) < 12 || len(newPassword) > 128 {
		return domain.NewError(domain.ErrCodeValidation, "password must be 12-128 characters")
	}
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return domain.NewError(domain.ErrCodeNotFound, "user not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return domain.NewError(domain.ErrCodeAuthInvalidCredentials, "invalid current password")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
	if err != nil {
		return domain.WrapError(domain.ErrCodeInternal, "failed to hash password", err)
	}

	if err := s.repo.UpdatePasswordHash(ctx, userID, string(hash)); err != nil {
		return domain.WrapError(domain.ErrCodeInternal, "failed to update password", err)
	}

	_ = s.repo.RevokeAllUserRefreshTokens(ctx, userID)

	return nil
}

func (s *Service) GetUser(ctx context.Context, userID uuid.UUID) (*User, error) {
	return s.repo.GetUserByID(ctx, userID)
}

func (s *Service) ResetAdminPassword(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return fmt.Errorf("admin email and password required")
	}
	if len(password) < 12 {
		return fmt.Errorf("admin password too short")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		u := &User{
			ID:           uuid.New(),
			Email:        email,
			PasswordHash: string(hash),
			Role:         "admin",
			Status:       "ACTIVE",
		}
		if err := s.repo.CreateUser(ctx, u); err != nil {
			return err
		}
		return nil
	}
	if err := s.repo.UpdatePasswordHash(ctx, user.ID, string(hash)); err != nil {
		return err
	}
	_ = s.repo.RevokeAllUserRefreshTokens(ctx, user.ID)
	return nil
}

func (s *Service) RevokeAllRefreshTokens(ctx context.Context) error {
	return s.repo.RevokeAllRefreshTokensGlobal(ctx)
}

func (s *Service) EnsureAdmin(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return nil
	}

	exists, err := s.repo.ExistsByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("check admin email: %w", err)
	}
	if exists {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	user := &User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: string(hash),
		Role:         "admin",
		Status:       "ACTIVE",
	}

	if err := s.repo.CreateUser(ctx, user); err != nil {
		return fmt.Errorf("create admin user: %w", err)
	}

	return nil
}

func (s *Service) generateAuthResponse(ctx context.Context, user *User) (*AuthResponse, error) {
	tenantID := ""
	if user.TenantID != nil {
		tenantID = user.TenantID.String()
	}
	accessToken, err := s.generateToken(user.ID.String(), tenantID, user.Role, s.config.JWTExpiration)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to generate access token", err)
	}

	refreshTokenStr, err := generateRefreshToken()
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to generate refresh token", err)
	}

	refreshToken := &RefreshToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: hashToken(refreshTokenStr),
		FamilyID:  uuid.New(),
		ExpiresAt: time.Now().Add(s.config.RefreshExpiration),
		Revoked:   false,
	}

	if err := s.repo.CreateRefreshToken(ctx, refreshToken); err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to save refresh token", err)
	}

	return &AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshTokenStr,
		ExpiresAt:    time.Now().Add(s.config.JWTExpiration).Unix(),
		User: &UserResponse{
			ID:         user.ID.String(),
			Email:      user.Email,
			Role:       user.Role,
			Status:     user.Status,
			AuthMethod: user.AuthMethod,
			CreatedAt:  user.CreatedAt.Format(time.RFC3339),
		},
	}, nil
}

func (s *Service) generateToken(userID, tenantID, role string, expiration time.Duration) (string, error) {
	claims := &Claims{
		UserID:   userID,
		TenantID: tenantID,
		Role:     role,
		Type:     "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "arbitrage-platform",
			Audience:  jwt.ClaimStrings{"arbitrage-api"},
			ID:        uuid.New().String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.config.JWTSecret))
}

func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.config.JWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("arbitrage-platform"), jwt.WithAudience("arbitrage-api"), jwt.WithLeeway(30*time.Second))

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	if claims.Type != "access" {
		return nil, fmt.Errorf("invalid token type")
	}
	return claims, nil
}

func (s *Service) GenerateAccessToken(userID, tenantID, role string) (string, error) {
	if role == "" {
		role = "user"
	}
	return s.generateToken(userID, tenantID, role, s.config.JWTExpiration)
}

func (s *Service) DeleteExpiredRefreshTokens(ctx context.Context) (int64, error) {
	return s.repo.DeleteExpiredRefreshTokens(ctx)
}

func (s *Service) CookieSecure() bool {
	if s.config.CookieSecure {
		return true
	}
	return s.config.CookieSecure
}

func (s *Service) CookieName() string {
	name := s.config.CookieName
	if name == "" {
		name = "__Host-refresh"
	}
	if !s.CookieSecure() && name == "__Host-refresh" {
		return "refresh_token"
	}
	return name
}

func (s *Service) CookieMaxAge() int {
	if s.config.CookieMaxAge > 0 {
		return s.config.CookieMaxAge
	}
	return 604800
}

// StartCleanupWorker runs DeleteExpiredRefreshTokens periodically.
// Call with a cancellable context; it blocks until ctx is done.
func (s *Service) StartCleanupWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := s.DeleteExpiredRefreshTokens(ctx)
			if err != nil {
				fmt.Printf("[auth] cleanup refresh tokens: %v\n", err)
			} else if n > 0 {
				fmt.Printf("[auth] cleaned %d expired/revoked refresh tokens\n", n)
			}
		}
	}
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func generateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
