package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

type Service struct {
	config *config.AuthConfig
	repo   *Repository
}

func NewService(cfg *config.AuthConfig, repo *Repository) *Service {
	return &Service{config: cfg, repo: repo}
}

func (s *Service) Register(ctx context.Context, req *RegisterRequest) (*AuthResponse, error) {
	exists, err := s.repo.ExistsByEmail(ctx, req.Email)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to check email", err)
	}
	if exists {
		return nil, domain.NewError(domain.ErrCodeConflict, "email already registered")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
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

	return s.generateAuthResponse(ctx, user)
}

func (s *Service) Refresh(ctx context.Context, refreshTokenStr string) (*AuthResponse, error) {
	tokenHash := hashToken(refreshTokenStr)

	refreshToken, err := s.repo.GetRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeAuthTokenInvalid, "invalid refresh token")
	}

	if refreshToken.Revoked {
		return nil, domain.NewError(domain.ErrCodeAuthRefreshRevoked, "refresh token has been revoked")
	}

	if time.Now().After(refreshToken.ExpiresAt) {
		return nil, domain.NewError(domain.ErrCodeAuthTokenExpired, "refresh token expired")
	}

	user, err := s.repo.GetUserByID(ctx, refreshToken.UserID)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeNotFound, "user not found")
	}

	if user.Status == "DISABLED" {
		return nil, domain.NewError(domain.ErrCodeAuthDisabled, "account is disabled")
	}

	_ = s.repo.RevokeRefreshToken(ctx, tokenHash)

	return s.generateAuthResponse(ctx, user)
}

func (s *Service) Logout(ctx context.Context, userID uuid.UUID) error {
	return s.repo.RevokeAllUserRefreshTokens(ctx, userID)
}

func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, oldPassword, newPassword string) error {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return domain.NewError(domain.ErrCodeNotFound, "user not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return domain.NewError(domain.ErrCodeAuthInvalidCredentials, "invalid current password")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
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

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
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
	accessToken, err := s.generateToken(user.ID.String(), "", user.Role, s.config.JWTExpiration)
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
			ID:        user.ID.String(),
			Email:     user.Email,
			Role:      user.Role,
			Status:    user.Status,
			CreatedAt: user.CreatedAt.Format(time.RFC3339),
		},
	}, nil
}

func (s *Service) generateToken(userID, tenantID, role string, expiration time.Duration) (string, error) {
	claims := &Claims{
		UserID:   userID,
		TenantID: tenantID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "arbitrage-platform",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.config.JWTSecret))
}

func (s *Service) GenerateTokenPair(userID, tenantID, role string) (*TokenPair, error) {
	if role == "" {
		role = "user"
	}

	accessToken, err := s.generateToken(userID, tenantID, role, s.config.JWTExpiration)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	refreshToken, err := s.generateToken(userID, tenantID, role, s.config.RefreshExpiration)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(s.config.JWTExpiration).Unix(),
	}, nil
}

func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.config.JWTSecret), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
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
