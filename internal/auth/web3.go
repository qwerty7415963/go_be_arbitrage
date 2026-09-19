package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	siwe "github.com/signinwithethereum/siwe-go"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

var SupportedChainNames = map[int64]string{
	1:          "Ethereum",
	42161:      "Arbitrum One",
	10:         "Optimism",
	137:        "Polygon",
	8453:       "Base",
	56:         "BNB Chain",
	43114:      "Avalanche",
	250:        "Fantom",
	100:        "Gnosis",
	42220:      "Celo",
	1313161554: "Aurora",
}

type Web3Service struct {
	config *config.AuthConfig
	repo   *Web3Repository
	auth   *Service // for JWT generation
}

func NewWeb3Service(cfg *config.AuthConfig, repo *Web3Repository, authService *Service) *Web3Service {
	return &Web3Service{config: cfg, repo: repo, auth: authService}
}

func (s *Web3Service) GenerateNonce(ctx context.Context, address string, chainID int64) (*WalletNonce, error) {
	address = normalizeAddress(address)

	if !s.isChainSupported(chainID) {
		return nil, domain.NewError(domain.ErrCodeAuthUnsupportedChain,
			fmt.Sprintf("chain %d is not supported", chainID))
	}

	if !isValidEthAddress(address) {
		return nil, domain.NewError(domain.ErrCodeValidation, "invalid Ethereum address format")
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to generate nonce", err)
	}
	nonce := hex.EncodeToString(b)

	nonceRecord := &WalletNonce{
		ID:        uuid.New(),
		Address:   address,
		ChainID:   chainID,
		Nonce:     nonce,
		ExpiresAt: time.Now().Add(s.config.SIWENonceTTL),
	}

	if err := s.repo.CreateNonce(ctx, nonceRecord); err != nil {
		return nil, domain.WrapError(domain.ErrCodeInternal, "failed to store nonce", err)
	}

	return nonceRecord, nil
}

func (s *Web3Service) VerifySignature(ctx context.Context, message string, signature string) (*AuthResponse, error) {
	msg, err := siwe.ParseMessage(message)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeAuthInvalidSignature, "invalid SIWE message format")
	}

	if msg.Domain != s.config.SIWEDomain {
		return nil, domain.NewError(domain.ErrCodeAuthInvalidSignature,
			fmt.Sprintf("domain mismatch: expected %s, got %s", s.config.SIWEDomain, msg.Domain))
	}

	chainID := int64(msg.ChainID)
	if !s.isChainSupported(chainID) {
		return nil, domain.NewError(domain.ErrCodeAuthUnsupportedChain,
			fmt.Sprintf("chain %d is not supported", chainID))
	}

	valid, err := msg.ValidNow()
	if err != nil || !valid {
		return nil, domain.NewError(domain.ErrCodeAuthTokenExpired, "SIWE message expired or not yet valid")
	}

	addr := normalizeAddress(msg.Address.Hex())
	nonceRecord, err := s.repo.GetValidNonce(ctx, msg.Nonce, addr, chainID)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeAuthNonceExpired, "nonce expired or not found")
	}
	if nonceRecord.Used {
		return nil, domain.NewError(domain.ErrCodeAuthNonceAlreadyUsed, "nonce already used")
	}

	pubKey, err := msg.VerifyEIP191(signature)
	if err != nil {
		return nil, domain.WrapError(domain.ErrCodeAuthInvalidSignature, "signature verification failed", err)
	}

	recoveredAddr := crypto.PubkeyToAddress(*pubKey)
	if !strings.EqualFold(recoveredAddr.Hex(), msg.Address.Hex()) {
		return nil, domain.NewError(domain.ErrCodeAuthInvalidSignature,
			fmt.Sprintf("signature address mismatch: recovered %s, expected %s", recoveredAddr.Hex(), msg.Address.Hex()))
	}

	_ = s.repo.MarkNonceUsed(ctx, nonceRecord.ID)

	user, err := s.repo.GetUserByAddress(ctx, addr, chainID)
	if err != nil {
		user = &User{
			ID:         uuid.New(),
			AuthMethod: "wallet",
			Role:       "user",
			Status:     "ACTIVE",
		}
		wallet := &WalletAddress{
			ID:        uuid.New(),
			Address:   addr,
			ChainID:   chainID,
			IsPrimary: true,
		}
		if err := s.repo.CreateUserWithWallet(ctx, user, wallet); err != nil {
			return nil, domain.WrapError(domain.ErrCodeInternal, "failed to create user", err)
		}
	}

	_ = s.auth.repo.UpdateLastLogin(ctx, user.ID)

	return s.auth.generateAuthResponse(ctx, user)
}

func (s *Web3Service) LinkWallet(ctx context.Context, userID uuid.UUID, address string, chainID int64, message string, signature string) error {
	address = normalizeAddress(address)

	if !s.isChainSupported(chainID) {
		return domain.NewError(domain.ErrCodeAuthUnsupportedChain,
			fmt.Sprintf("chain %d is not supported", chainID))
	}

	msg, err := siwe.ParseMessage(message)
	if err != nil {
		return domain.NewError(domain.ErrCodeAuthInvalidSignature, "invalid SIWE message format")
	}

	if msg.Domain != s.config.SIWEDomain {
		return domain.NewError(domain.ErrCodeAuthInvalidSignature, "domain mismatch")
	}

	if int64(msg.ChainID) != chainID {
		return domain.NewError(domain.ErrCodeAuthInvalidSignature, "chain_id mismatch in message")
	}

	if !strings.EqualFold(msg.Address.Hex(), address) {
		return domain.NewError(domain.ErrCodeAuthInvalidSignature, "address mismatch in message")
	}

	valid, err := msg.ValidNow()
	if err != nil || !valid {
		return domain.NewError(domain.ErrCodeAuthTokenExpired, "SIWE message expired")
	}

	nonceRecord, err := s.repo.GetValidNonce(ctx, msg.Nonce, address, chainID)
	if err != nil || nonceRecord.Used {
		return domain.NewError(domain.ErrCodeAuthNonceExpired, "nonce expired or already used")
	}

	pubKey, err := msg.VerifyEIP191(signature)
	if err != nil {
		return domain.WrapError(domain.ErrCodeAuthInvalidSignature, "signature verification failed", err)
	}

	recoveredAddr := crypto.PubkeyToAddress(*pubKey)
	if !strings.EqualFold(recoveredAddr.Hex(), msg.Address.Hex()) {
		return domain.NewError(domain.ErrCodeAuthInvalidSignature, "signature address mismatch")
	}

	_ = s.repo.MarkNonceUsed(ctx, nonceRecord.ID)

	existing, err := s.repo.GetWalletByAddress(ctx, address, chainID)
	if err == nil && existing != nil && existing.UserID != userID {
		return domain.NewError(domain.ErrCodeAuthWalletAlreadyLinked,
			"wallet is already linked to another account")
	}

	owned, _ := s.repo.IsWalletOwnedByUser(ctx, userID, address, chainID)
	if owned {
		return nil
	}

	wallet := &WalletAddress{
		ID:        uuid.New(),
		UserID:    userID,
		Address:   address,
		ChainID:   chainID,
		IsPrimary: false,
	}
	if err := s.repo.CreateWallet(ctx, wallet); err != nil {
		return domain.WrapError(domain.ErrCodeInternal, "failed to link wallet", err)
	}

	user, err := s.auth.repo.GetUserByID(ctx, userID)
	if err == nil && user.Email != "" && user.AuthMethod == "email" {
		_ = s.auth.repo.UpdateAuthMethod(ctx, userID, "both")
	}

	return nil
}

func (s *Web3Service) UnlinkWallet(ctx context.Context, userID uuid.UUID, walletID uuid.UUID) error {
	// Verify ownership
	owned, err := s.repo.IsWalletOwnedBy(ctx, userID, walletID)
	if err != nil || !owned {
		return domain.NewError(domain.ErrCodeAuthWalletNotFound, "wallet not found or not owned by user")
	}

	wallets, err := s.repo.GetWalletsByUserID(ctx, userID)
	if err != nil {
		return domain.WrapError(domain.ErrCodeInternal, "failed to fetch wallets", err)
	}
	if len(wallets) <= 1 {
		return domain.NewError(domain.ErrCodeValidation, "cannot unlink the last wallet")
	}

	if err := s.repo.DeleteWallet(ctx, walletID); err != nil {
		return domain.WrapError(domain.ErrCodeInternal, "failed to unlink wallet", err)
	}

	return nil
}

func (s *Web3Service) ListWallets(ctx context.Context, userID uuid.UUID) ([]*WalletAddress, error) {
	return s.repo.GetWalletsByUserID(ctx, userID)
}

func (s *Web3Service) isChainSupported(chainID int64) bool {
	for _, id := range s.config.SupportedChains {
		if id == chainID {
			return true
		}
	}
	return false
}

func normalizeAddress(address string) string {
	return strings.ToLower(address)
}

func isValidEthAddress(address string) bool {
	if !strings.HasPrefix(address, "0x") {
		return false
	}
	if len(address) != 42 {
		return false
	}
	for _, c := range address[2:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// Ensure these are used (referenced by web3_handler.go)
var _ = (*ecdsa.PublicKey)(nil)
var _ = crypto.PubkeyToAddress
