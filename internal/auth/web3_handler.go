package auth

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/api"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type Web3Handler struct {
	service *Web3Service
}

func NewWeb3Handler(service *Web3Service) *Web3Handler {
	return &Web3Handler{service: service}
}

// GetNonce godoc
// @Summary      Get SIWE nonce
// @Description  Generate a nonce for wallet signature verification
// @Tags         auth,web3
// @Accept       json
// @Produce      json
// @Param        body  body      WalletNonceRequest  true  "Wallet address and chain ID"
// @Success      200   {object}  api.Response{data=WalletNonce}
// @Failure      400   {object}  api.Response
// @Router       /api/v1/auth/wallet/nonce [post]
func (h *Web3Handler) GetNonce(c *gin.Context) {
	var req WalletNonceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, []api.FieldError{
			{Field: "body", Code: "INVALID", Message: err.Error()},
		})
		return
	}

	nonce, err := h.service.GenerateNonce(c.Request.Context(), req.Address, req.ChainID)
	if err != nil {
		if appErr, ok := err.(*domain.AppError); ok {
			api.RespondError(c, appErr)
		} else {
			api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "failed to generate nonce", err))
		}
		return
	}

	api.RespondSuccess(c, gin.H{
		"nonce":   nonce.Nonce,
		"message": formatSIWEMessage(nonce.Address, nonce.ChainID, nonce.Nonce, h.service.config.SIWEDomain),
		"expires_at": nonce.ExpiresAt.Unix(),
	})
}

// Verify godoc
// @Summary      Verify wallet signature
// @Description  Verify SIWE message + signature, returns JWT for new/existing user
// @Tags         auth,web3
// @Accept       json
// @Produce      json
// @Param        body  body      WalletVerifyRequest  true  "SIWE message and signature"
// @Success      200   {object}  api.Response{data=AuthResponse}
// @Failure      400   {object}  api.Response
// @Failure      401   {object}  api.Response
// @Router       /api/v1/auth/wallet/verify [post]
func (h *Web3Handler) Verify(c *gin.Context) {
	var req WalletVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, []api.FieldError{
			{Field: "body", Code: "INVALID", Message: err.Error()},
		})
		return
	}

	resp, err := h.service.VerifySignature(c.Request.Context(), req.Message, req.Signature)
	if err != nil {
		if appErr, ok := err.(*domain.AppError); ok {
			api.RespondError(c, appErr)
		} else {
			api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "verification failed", err))
		}
		return
	}

	api.RespondSuccess(c, resp)
}

// LinkWallet godoc
// @Summary      Link wallet to account
// @Description  Link a wallet address to the authenticated user
// @Tags         auth,web3
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      WalletLinkRequest  true  "Wallet data with signature"
// @Success      204   "No Content"
// @Failure      400   {object}  api.Response
// @Failure      401   {object}  api.Response
// @Router       /api/v1/auth/wallet/link [post]
func (h *Web3Handler) LinkWallet(c *gin.Context) {
	userIDStr, exists := c.Get("user_id")
	if !exists {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "user not authenticated"))
		return
	}
	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid user ID"))
		return
	}

	var req WalletLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondValidationError(c, []api.FieldError{
			{Field: "body", Code: "INVALID", Message: err.Error()},
		})
		return
	}

	if err := h.service.LinkWallet(c.Request.Context(), userID, req.Address, req.ChainID, req.Message, req.Signature); err != nil {
		if appErr, ok := err.(*domain.AppError); ok {
			api.RespondError(c, appErr)
		} else {
			api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "failed to link wallet", err))
		}
		return
	}

	api.RespondNoContent(c)
}

// UnlinkWallet godoc
// @Summary      Unlink wallet from account
// @Description  Remove a wallet from the authenticated user
// @Tags         auth,web3
// @Security     BearerAuth
// @Produce      json
// @Param        wallet_id  path      string  true  "Wallet ID"
// @Success      204   "No Content"
// @Failure      400   {object}  api.Response
// @Failure      401   {object}  api.Response
// @Router       /api/v1/auth/wallet/{wallet_id} [delete]
func (h *Web3Handler) UnlinkWallet(c *gin.Context) {
	userIDStr, exists := c.Get("user_id")
	if !exists {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "user not authenticated"))
		return
	}
	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid user ID"))
		return
	}

	walletIDStr := c.Param("wallet_id")
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid wallet ID"))
		return
	}

	if err := h.service.UnlinkWallet(c.Request.Context(), userID, walletID); err != nil {
		if appErr, ok := err.(*domain.AppError); ok {
			api.RespondError(c, appErr)
		} else {
			api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "failed to unlink wallet", err))
		}
		return
	}

	api.RespondNoContent(c)
}

// ListWallets godoc
// @Summary      List user's wallets
// @Description  Get all wallet addresses linked to the authenticated user
// @Tags         auth,web3
// @Security     BearerAuth
// @Produce      json
// @Success      200   {object}  api.Response{data=[]WalletResponse}
// @Failure      401   {object}  api.Response
// @Router       /api/v1/auth/wallet/list [get]
func (h *Web3Handler) ListWallets(c *gin.Context) {
	userIDStr, exists := c.Get("user_id")
	if !exists {
		api.RespondError(c, domain.NewError(domain.ErrCodeAuthForbidden, "user not authenticated"))
		return
	}
	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		api.RespondError(c, domain.NewError(domain.ErrCodeValidation, "invalid user ID"))
		return
	}

	wallets, err := h.service.ListWallets(c.Request.Context(), userID)
	if err != nil {
		api.RespondError(c, domain.WrapError(domain.ErrCodeInternal, "failed to list wallets", err))
		return
	}

	resp := make([]*WalletResponse, 0, len(wallets))
	for _, w := range wallets {
		chainName := SupportedChainNames[w.ChainID]
		if chainName == "" {
			chainName = fmt.Sprintf("Chain %d", w.ChainID)
		}
		resp = append(resp, &WalletResponse{
			ID:         w.ID.String(),
			Address:    w.Address,
			ChainID:    w.ChainID,
			IsPrimary:  w.IsPrimary,
			VerifiedAt: w.VerifiedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	api.RespondSuccess(c, resp)
}

// formatSIWEMessage returns the SIWE message string that the client should sign.
func formatSIWEMessage(address string, chainID int64, nonce string, domain string) string {
	chainName := SupportedChainNames[chainID]
	if chainName == "" {
		chainName = fmt.Sprintf("Chain %d", chainID)
	}
	return fmt.Sprintf(
		"%s wants you to sign in with your Ethereum account:\n%s\n\nSign in to Arbitrage Platform\n\nURI: https://%s\nVersion: 1\nChain ID: %d\nNonce: %s\nIssued At: %s",
		domain,
		address,
		domain,
		chainID,
		nonce,
		time.Now().UTC().Format(time.RFC3339),
	)
}
