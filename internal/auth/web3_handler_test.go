package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
)

func setupWeb3TestDB(t *testing.T) (*pgxpool.Pool, *Web3Handler) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping: requires PostgreSQL")
	}
	host := os.Getenv("ARBITRAGE_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ARBITRAGE_DB_PORT")
	if port == "" {
		port = "5433"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dsn := fmt.Sprintf("postgres://test:test@%s:%s/arbitrage_test?sslmode=disable", host, port)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to test DB: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping test DB: %v", err)
	}
	pool.Exec(ctx, "DELETE FROM wallet_nonces WHERE address LIKE '0xw3handler%'")
	pool.Exec(ctx, "DELETE FROM wallet_addresses WHERE address LIKE '0xw3handler%'")
	pool.Exec(ctx, "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE 'w3handler-test%')")
	pool.Exec(ctx, "DELETE FROM users WHERE email LIKE 'w3handler-test%'")
	pool.Exec(ctx, "DELETE FROM users WHERE auth_method = 'wallet'")

	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM wallet_nonces WHERE address LIKE '0xw3handler%'")
		pool.Exec(context.Background(), "DELETE FROM wallet_addresses WHERE address LIKE '0xw3handler%'")
		pool.Exec(context.Background(), "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE 'w3handler-test%')")
		pool.Exec(context.Background(), "DELETE FROM users WHERE email LIKE 'w3handler-test%'")
		pool.Exec(context.Background(), "DELETE FROM users WHERE auth_method = 'wallet'")
		pool.Close()
	})

	cfg := &config.AuthConfig{
		JWTSecret:         "w3handler-test-secret",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
		SIWEDomain:        "localhost",
		SIWENonceTTL:      5 * time.Minute,
		SupportedChains:   []int64{1, 42161, 10, 137, 8453},
	}
	authRepo := NewRepository(pool)
	authSvc := NewService(cfg, authRepo)
	web3Repo := NewWeb3Repository(pool)
	web3Svc := NewWeb3Service(cfg, web3Repo, authSvc)
	web3Handler := NewWeb3Handler(web3Svc)

	return pool, web3Handler
}

func setupWeb3Router(handler *Web3Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	authRoutes := v1.Group("/auth")
	{
		authRoutes.POST("/wallet/nonce", handler.GetNonce)
		authRoutes.POST("/wallet/verify", handler.Verify)

		protected := authRoutes.Group("")
		{
			protected.POST("/wallet/link", func(c *gin.Context) {
				c.Set("user_id", "00000000-0000-0000-0000-000000000000")
				c.Next()
			}, handler.LinkWallet)
			protected.DELETE("/wallet/:wallet_id", func(c *gin.Context) {
				c.Set("user_id", "00000000-0000-0000-0000-000000000000")
				c.Next()
			}, handler.UnlinkWallet)
			protected.GET("/wallet/list", func(c *gin.Context) {
				c.Set("user_id", "00000000-0000-0000-0000-000000000000")
				c.Next()
			}, handler.ListWallets)
		}
	}
	return router
}

func TestWeb3Handler_GetNonce_Success(t *testing.T) {
	_, handler := setupWeb3TestDB(t)
	router := setupWeb3Router(handler)

	body := map[string]interface{}{
		"address":  "0xw3handler00112233445566778899aabbccddeeff00",
		"chain_id": 1,
	}
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/api/v1/auth/wallet/nonce", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	if data["nonce"] == nil || data["nonce"] == "" {
		t.Error("expected nonce in response")
	}
	if data["message"] == nil || data["message"] == "" {
		t.Error("expected message in response")
	}
}

func TestWeb3Handler_GetNonce_InvalidAddress(t *testing.T) {
	_, handler := setupWeb3TestDB(t)
	router := setupWeb3Router(handler)

	body := map[string]interface{}{
		"address":  "not-an-address",
		"chain_id": 1,
	}
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/api/v1/auth/wallet/nonce", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWeb3Handler_GetNonce_UnsupportedChain(t *testing.T) {
	_, handler := setupWeb3TestDB(t)
	router := setupWeb3Router(handler)

	body := map[string]interface{}{
		"address":  "0xw3handler00112233445566778899aabbccddeeff00",
		"chain_id": 42,
	}
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/api/v1/auth/wallet/nonce", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWeb3Handler_GetNonce_InvalidBody(t *testing.T) {
	_, handler := setupWeb3TestDB(t)
	router := setupWeb3Router(handler)

	req, _ := http.NewRequest("POST", "/api/v1/auth/wallet/nonce", bytes.NewBuffer([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestWeb3Handler_Verify_InvalidMessage(t *testing.T) {
	_, handler := setupWeb3TestDB(t)
	router := setupWeb3Router(handler)

	body := map[string]string{
		"message":   "not-a-valid-siwe-message",
		"signature": "0xsomething",
	}
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/api/v1/auth/wallet/verify", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWeb3Handler_Verify_InvalidBody(t *testing.T) {
	_, handler := setupWeb3TestDB(t)
	router := setupWeb3Router(handler)

	req, _ := http.NewRequest("POST", "/api/v1/auth/wallet/verify", bytes.NewBuffer([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestWeb3Handler_LinkWallet_NoAuth(t *testing.T) {
	_, handler := setupWeb3TestDB(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.POST("/auth/wallet/link", handler.LinkWallet)

	body := map[string]string{"address": "0x1234", "message": "msg", "signature": "0xsig"}
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/api/v1/auth/wallet/link", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestWeb3Handler_UnlinkWallet_NoAuth(t *testing.T) {
	_, handler := setupWeb3TestDB(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.DELETE("/auth/wallet/:wallet_id", handler.UnlinkWallet)

	req, _ := http.NewRequest("DELETE", "/api/v1/auth/wallet/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestWeb3Handler_ListWallets_NoAuth(t *testing.T) {
	_, handler := setupWeb3TestDB(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.GET("/auth/wallet/list", handler.ListWallets)

	req, _ := http.NewRequest("GET", "/api/v1/auth/wallet/list", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestWeb3Handler_ListWallets_Empty(t *testing.T) {
	pool, handler := setupWeb3TestDB(t)

	// Create a user with no wallets
	var userID string
	pool.QueryRow(context.Background(), "INSERT INTO users (id, auth_method, role, status) VALUES ($1, 'wallet', 'user', 'ACTIVE') RETURNING id::text", uuid.New()).Scan(&userID)

	walletRouter := gin.New()
	v1 := walletRouter.Group("/api/v1")
	v1.GET("/auth/wallet/list", func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Next()
	}, handler.ListWallets)
	_ = pool

	req, _ := http.NewRequest("GET", "/api/v1/auth/wallet/list", nil)
	w := httptest.NewRecorder()
	walletRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].([]interface{})
	if len(data) != 0 {
		t.Errorf("expected empty list, got %d", len(data))
	}
}
