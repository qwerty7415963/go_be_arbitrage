package auth

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	TenantID     *uuid.UUID `json:"tenant_id,omitempty" db:"tenant_id"`
	Email        string     `json:"email" db:"email"`
	PasswordHash string     `json:"-" db:"password_hash"`
	Role         string     `json:"role" db:"role"`
	Status       string     `json:"status" db:"status"`
	AuthMethod   string     `json:"auth_method" db:"auth_method"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
}

type WalletAddress struct {
	ID         uuid.UUID `json:"id" db:"id"`
	UserID     uuid.UUID `json:"user_id" db:"user_id"`
	Address    string    `json:"address" db:"address"`
	ChainID    int64     `json:"chain_id" db:"chain_id"`
	IsPrimary  bool      `json:"is_primary" db:"is_primary"`
	VerifiedAt time.Time `json:"verified_at" db:"verified_at"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

type WalletNonce struct {
	ID        uuid.UUID `json:"id" db:"id"`
	Address   string    `json:"address" db:"address"`
	ChainID   int64     `json:"chain_id" db:"chain_id"`
	Nonce     string    `json:"nonce" db:"nonce"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
	Used      bool      `json:"used" db:"used"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type RefreshToken struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
	CreatedAt time.Time `json:"created_at"`
}

type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=128"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=128"`
}

type AuthResponse struct {
	AccessToken  string        `json:"access_token"`
	RefreshToken string        `json:"refresh_token"`
	ExpiresAt    int64         `json:"expires_at"`
	User         *UserResponse `json:"user"`
}

type UserResponse struct {
	ID         string `json:"id"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	AuthMethod string `json:"auth_method"`
	CreatedAt  string `json:"created_at"`
}

type WalletNonceRequest struct {
	Address string `json:"address" binding:"required"`
	ChainID int64  `json:"chain_id" binding:"required"`
}

type WalletVerifyRequest struct {
	Message   string `json:"message" binding:"required"`
	Signature string `json:"signature" binding:"required"`
}

type WalletLinkRequest struct {
	Address   string `json:"address" binding:"required"`
	ChainID   int64  `json:"chain_id" binding:"required"`
	Message   string `json:"message" binding:"required"`
	Signature string `json:"signature" binding:"required"`
}

type WalletResponse struct {
	ID         string `json:"id"`
	Address    string `json:"address"`
	ChainID    int64  `json:"chain_id"`
	IsPrimary  bool   `json:"is_primary"`
	VerifiedAt string `json:"verified_at"`
}
