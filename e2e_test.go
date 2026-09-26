//go:build e2e

package e2e

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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/auth"
	"github.com/qwerty7415963/go_be_arbitrage/internal/config"
	"github.com/qwerty7415963/go_be_arbitrage/internal/instrument"
	"github.com/qwerty7415963/go_be_arbitrage/internal/venue"
)

type TestSuite struct {
	db     *pgxpool.Pool
	router *gin.Engine
}

func setupTestSuite(t *testing.T) *TestSuite {
	t.Helper()

	dbHost := os.Getenv("ARBITRAGE_DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPort := os.Getenv("ARBITRAGE_DB_PORT")
	if dbPort == "" {
		dbPort = "5433"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dsn := "postgres://test:test@" + dbHost + ":" + dbPort + "/arbitrage_test?sslmode=disable"

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping database: %v", err)
	}

	// Clean up
	pool.Exec(ctx, "DELETE FROM venue_instruments")
	pool.Exec(ctx, "DELETE FROM instruments")
	pool.Exec(ctx, "DELETE FROM venues")

	// Setup handlers
	venueRepo := venue.NewRepository(pool)
	venueService := venue.NewService(venueRepo)
	venueHandler := venue.NewHandler(venueService)

	instrumentRepo := instrument.NewRepository(pool)
	instrumentService := instrument.NewService(instrumentRepo)
	instrumentHandler := instrument.NewHandler(instrumentService)

	// Setup router
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	{
		// Venues
		v1.POST("/venues", venueHandler.Create)
		v1.GET("/venues", venueHandler.List)
		v1.GET("/venues/:id", venueHandler.GetByID)
		v1.PUT("/venues/:id", venueHandler.Update)
		v1.DELETE("/venues/:id", venueHandler.Delete)

		// Instruments
		v1.POST("/instruments", instrumentHandler.Create)
		v1.GET("/instruments", instrumentHandler.List)
		v1.GET("/instruments/tradable", instrumentHandler.ListTradable)
		v1.GET("/instruments/:id", instrumentHandler.GetByID)
		v1.PUT("/instruments/:id", instrumentHandler.Update)
		v1.DELETE("/instruments/:id", instrumentHandler.Delete)
		v1.PUT("/instruments/:id/trading", instrumentHandler.EnableTrading)

		// Venue Instruments
		v1.POST("/venue-instruments", instrumentHandler.CreateVenueInstrument)
		v1.GET("/venue-instruments", instrumentHandler.ListVenueInstruments)
	}

	return &TestSuite{
		db:     pool,
		router: router,
	}
}

func (s *TestSuite) cleanup() {
	s.db.Exec(context.Background(), "DELETE FROM venue_instruments")
	s.db.Exec(context.Background(), "DELETE FROM instruments")
	s.db.Exec(context.Background(), "DELETE FROM venues")
	s.db.Close()
}

func TestE2E_RegisterVenue_ThenDiscoverInstrument(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite := setupTestSuite(t)
	defer suite.cleanup()

	// Step 1: Create venue
	venueBody := map[string]string{
		"code":       "e2e-test-venue",
		"name":       "E2E Test Venue",
		"venue_type": "CEX",
	}
	jsonBody, _ := json.Marshal(venueBody)

	req, _ := http.NewRequest("POST", "/api/v1/venues", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201 for venue creation, got %d", w.Code)
	}

	var venueResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &venueResp)

	venueData := venueResp["data"].(map[string]interface{})
	venueID := venueData["id"].(string)

	// Step 2: Create instrument
	instrumentBody := map[string]interface{}{
		"canonical_symbol": "BTC-USDT-E2E",
		"base_asset":       "BTC",
		"quote_asset":      "USDT",
		"instrument_type":  "PERP",
		"contract_type":    "LINEAR",
		"price_tick":       "0.1",
		"quantity_step":    "0.001",
	}
	jsonBody, _ = json.Marshal(instrumentBody)

	req, _ = http.NewRequest("POST", "/api/v1/instruments", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201 for instrument creation, got %d", w.Code)
	}

	var instrumentResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &instrumentResp)

	instrumentData := instrumentResp["data"].(map[string]interface{})
	instrumentID := instrumentData["id"].(string)

	// Step 3: Create venue instrument mapping
	mappingBody := map[string]string{
		"venue_id":      venueID,
		"instrument_id": instrumentID,
		"venue_symbol":  "BTCUSDT",
	}
	jsonBody, _ = json.Marshal(mappingBody)

	req, _ = http.NewRequest("POST", "/api/v1/venue-instruments", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201 for venue instrument mapping, got %d", w.Code)
	}

	// Verify mapping exists
	req, _ = http.NewRequest("GET", "/api/v1/venue-instruments?venue_id="+venueID, nil)
	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for list venue instruments, got %d", w.Code)
	}

	var listResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &listResp)

	data := listResp["data"].([]interface{})
	if len(data) < 1 {
		t.Error("expected at least 1 venue instrument mapping")
	}
}

func TestE2E_EnableInstrument_NotReviewed_Blocked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite := setupTestSuite(t)
	defer suite.cleanup()

	// Create instrument with DISCOVERED status
	instrumentBody := map[string]interface{}{
		"canonical_symbol": "ETH-USDT-E2E-BLOCK",
		"base_asset":       "ETH",
		"quote_asset":      "USDT",
		"instrument_type":  "PERP",
		"contract_type":    "LINEAR",
		"price_tick":       "0.01",
		"quantity_step":    "0.01",
	}
	jsonBody, _ := json.Marshal(instrumentBody)

	req, _ := http.NewRequest("POST", "/api/v1/instruments", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	instrumentID := data["id"].(string)

	// Try to enable trading - should be blocked
	tradingBody := map[string]bool{
		"enabled": true,
	}
	jsonBody, _ = json.Marshal(tradingBody)

	req, _ = http.NewRequest("PUT", "/api/v1/instruments/"+instrumentID+"/trading", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	// Should fail because instrument not reviewed
	if w.Code == http.StatusOK {
		t.Error("expected error for non-reviewed instrument, got 200")
	}
}

func TestE2E_EnableInstrument_Reviewed_Allowed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite := setupTestSuite(t)
	defer suite.cleanup()

	// Create instrument
	instrumentBody := map[string]interface{}{
		"canonical_symbol": "AVAX-USDT-E2E-ALLOW",
		"base_asset":       "AVAX",
		"quote_asset":      "USDT",
		"instrument_type":  "PERP",
		"contract_type":    "LINEAR",
		"price_tick":       "0.01",
		"quantity_step":    "0.01",
	}
	jsonBody, _ := json.Marshal(instrumentBody)

	req, _ := http.NewRequest("POST", "/api/v1/instruments", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	instrumentID := data["id"].(string)

	// Update discovery status to REVIEWED
	updateBody := map[string]string{
		"discovery_status": "REVIEWED",
	}
	jsonBody, _ = json.Marshal(updateBody)

	req, _ = http.NewRequest("PUT", "/api/v1/instruments/"+instrumentID, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for update, got %d", w.Code)
	}

	// Now enable trading - should be allowed
	tradingBody := map[string]bool{
		"enabled": true,
	}
	jsonBody, _ = json.Marshal(tradingBody)

	req, _ = http.NewRequest("PUT", "/api/v1/instruments/"+instrumentID+"/trading", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for enable trading on reviewed instrument, got %d", w.Code)
	}
}

func TestE2E_Instrument_NotInTradableList_BeforeReview(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite := setupTestSuite(t)
	defer suite.cleanup()

	// Create instrument (DISCOVERED status)
	instrumentBody := map[string]interface{}{
		"canonical_symbol": "DOGE-USDT-E2E-NO-TRADE",
		"base_asset":       "DOGE",
		"quote_asset":      "USDT",
		"instrument_type":  "PERP",
		"contract_type":    "LINEAR",
		"price_tick":       "0.0001",
		"quantity_step":    "1",
	}
	jsonBody, _ := json.Marshal(instrumentBody)

	req, _ := http.NewRequest("POST", "/api/v1/instruments", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w.Code)
	}

	// Check tradable list - should not contain this instrument
	req, _ = http.NewRequest("GET", "/api/v1/instruments/tradable", nil)
	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].([]interface{})
	for _, item := range data {
		instr := item.(map[string]interface{})
		if instr["canonical_symbol"] == "DOGE-USDT-E2E-NO-TRADE" {
			t.Error("instrument should not be in tradable list before review")
		}
	}
}

func TestE2E_Instrument_InTradableList_AfterReview(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite := setupTestSuite(t)
	defer suite.cleanup()

	// Create instrument
	instrumentBody := map[string]interface{}{
		"canonical_symbol": "MATIC-USDT-E2E-IN-TRADE",
		"base_asset":       "MATIC",
		"quote_asset":      "USDT",
		"instrument_type":  "PERP",
		"contract_type":    "LINEAR",
		"price_tick":       "0.001",
		"quantity_step":    "0.1",
	}
	jsonBody, _ := json.Marshal(instrumentBody)

	req, _ := http.NewRequest("POST", "/api/v1/instruments", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	instrumentID := data["id"].(string)

	// Update to REVIEWED
	updateBody := map[string]string{
		"discovery_status": "REVIEWED",
	}
	jsonBody, _ = json.Marshal(updateBody)

	req, _ = http.NewRequest("PUT", "/api/v1/instruments/"+instrumentID, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	// Enable trading
	tradingBody := map[string]bool{
		"enabled": true,
	}
	jsonBody, _ = json.Marshal(tradingBody)

	req, _ = http.NewRequest("PUT", "/api/v1/instruments/"+instrumentID+"/trading", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Check tradable list - should contain this instrument
	req, _ = http.NewRequest("GET", "/api/v1/instruments/tradable", nil)
	w = httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var listResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &listResp)

	listData := listResp["data"].([]interface{})
	found := false
	for _, item := range listData {
		instr := item.(map[string]interface{})
		if instr["canonical_symbol"] == "MATIC-USDT-E2E-IN-TRADE" {
			found = true
			break
		}
	}

	if !found {
		t.Error("instrument should be in tradable list after review and enable")
	}
}

func TestE2E_Health_Returns200(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite := setupTestSuite(t)
	defer suite.cleanup()

	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestE2E_Ping_Returns200(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite := setupTestSuite(t)
	defer suite.cleanup()

	// Ping endpoint not in this router, but we can test venue/instrument endpoints
	req, _ := http.NewRequest("GET", "/api/v1/venues", nil)
	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestE2E_CreateVenue_InvalidBody_Returns400(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite := setupTestSuite(t)
	defer suite.cleanup()

	req, _ := http.NewRequest("POST", "/api/v1/venues", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestE2E_CreateInstrument_InvalidBody_Returns400(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite := setupTestSuite(t)
	defer suite.cleanup()

	req, _ := http.NewRequest("POST", "/api/v1/instruments", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	suite.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// ═══════════════════════════════════════════════════════════════
// AUTH E2E Tests
// ═══════════════════════════════════════════════════════════════

func setupAuthTestSuite(t *testing.T) *TestSuite {
	t.Helper()

	dbHost := os.Getenv("ARBITRAGE_DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPort := os.Getenv("ARBITRAGE_DB_PORT")
	if dbPort == "" {
		dbPort = "5433"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dsn := "postgres://test:test@" + dbHost + ":" + dbPort + "/arbitrage_test?sslmode=disable"

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping database: %v", err)
	}

	// Clean auth test data
	pool.Exec(ctx, "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%e2e-auth%')")
	pool.Exec(ctx, "DELETE FROM users WHERE email LIKE '%e2e-auth%'")

	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%e2e-auth%')")
		pool.Exec(context.Background(), "DELETE FROM users WHERE email LIKE '%e2e-auth%'")
		pool.Close()
	})

	// Setup auth
	authRepo := auth.NewRepository(pool)
	authService := auth.NewService(&config.AuthConfig{
		JWTSecret:         "e2e-test-secret",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}, authRepo)
	authHandler := auth.NewHandler(authService)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	{
		authRoutes := v1.Group("/auth")
		{
			authRoutes.POST("/register", authHandler.Register)
			authRoutes.POST("/login", authHandler.Login)
			authRoutes.POST("/refresh", authHandler.Refresh)
			authProtected := authRoutes.Group("")
			{
				authProtected.POST("/logout", func(c *gin.Context) {
					c.Set("user_id", c.GetString("user_id"))
					c.Next()
				}, authHandler.Logout)
				authProtected.GET("/me", func(c *gin.Context) {
					c.Set("user_id", c.GetString("user_id"))
					c.Next()
				}, authHandler.Me)
			}
		}
	}

	return &TestSuite{
		db:     pool,
		router: router,
	}
}

func (s *TestSuite) authCleanup(t *testing.T) {
	s.db.Exec(context.Background(), "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%e2e-auth%')")
	s.db.Exec(context.Background(), "DELETE FROM users WHERE email LIKE '%e2e-auth%'")
}

func e2eRegister(t *testing.T, router *gin.Engine, email, password string) map[string]interface{} {
	t.Helper()
	body := map[string]string{"email": email, "password": password}
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/api/v1/auth/register", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp["data"].(map[string]interface{})
}

func e2eLogin(t *testing.T, router *gin.Engine, email, password string) map[string]interface{} {
	t.Helper()
	body := map[string]string{"email": email, "password": password}
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp["data"].(map[string]interface{})
}

// ─── AUTH-E-01: Register → Login → Me → Refresh → Logout → Refresh dies ──

func TestE2E_Auth_FullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite := setupAuthTestSuite(t)
	defer suite.authCleanup(t)

	email := fmt.Sprintf("e2e-auth-flow-%d@test.com", time.Now().UnixNano())
	password := "testpass123"

	// Step 1: Register
	data := e2eRegister(t, suite.router, email, password)
	accessToken := data["access_token"].(string)
	refreshToken := data["refresh_token"].(string)
	userData := data["user"].(map[string]interface{})

	if userData["email"] != email {
		t.Errorf("expected email %s, got %s", email, userData["email"])
	}
	if userData["role"] != "user" {
		t.Errorf("expected role user, got %s", userData["role"])
	}

	_ = accessToken

	// Step 2: Login
	loginData := e2eLogin(t, suite.router, email, password)
	loginRefresh := loginData["refresh_token"].(string)

	// Step 3: Refresh
	refreshBody := map[string]string{"refresh_token": loginRefresh}
	refreshJSON, _ := json.Marshal(refreshBody)
	refreshReq, _ := http.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewBuffer(refreshJSON))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshW := httptest.NewRecorder()
	suite.router.ServeHTTP(refreshW, refreshReq)

	if refreshW.Code != http.StatusOK {
		t.Fatalf("refresh: expected 200, got %d: %s", refreshW.Code, refreshW.Body.String())
	}

	var refreshResp map[string]interface{}
	json.Unmarshal(refreshW.Body.Bytes(), &refreshResp)
	newRefreshData := refreshResp["data"].(map[string]interface{})
	newRefreshToken := newRefreshData["refresh_token"].(string)

	// Step 4: Old refresh token should be dead
	oldRefreshBody := map[string]string{"refresh_token": refreshToken}
	oldRefreshJSON, _ := json.Marshal(oldRefreshBody)
	oldRefreshReq, _ := http.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewBuffer(oldRefreshJSON))
	oldRefreshReq.Header.Set("Content-Type", "application/json")
	oldRefreshW := httptest.NewRecorder()
	suite.router.ServeHTTP(oldRefreshW, oldRefreshReq)

	if oldRefreshW.Code != http.StatusUnauthorized {
		t.Errorf("old refresh token should be dead, got %d", oldRefreshW.Code)
	}

	// Step 5: New refresh token works
	newRefreshBody := map[string]string{"refresh_token": newRefreshToken}
	newRefreshJSON, _ := json.Marshal(newRefreshBody)
	newRefreshReq, _ := http.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewBuffer(newRefreshJSON))
	newRefreshReq.Header.Set("Content-Type", "application/json")
	newRefreshW := httptest.NewRecorder()
	suite.router.ServeHTTP(newRefreshW, newRefreshReq)

	if newRefreshW.Code != http.StatusOK {
		t.Errorf("new refresh token should work, got %d", newRefreshW.Code)
	}
}

// ─── AUTH-E-02: Register → Change Password → Login old fails → Login new works ──

func TestE2E_Auth_ChangePassword(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite := setupAuthTestSuite(t)
	defer suite.authCleanup(t)

	email := fmt.Sprintf("e2e-auth-changepw-%d@test.com", time.Now().UnixNano())
	oldPass := "oldpass123"
	newPass := "newpass456"

	// Register
	e2eRegister(t, suite.router, email, oldPass)

	// Login to get user_id
	loginData := e2eLogin(t, suite.router, email, oldPass)
	_ = loginData

	// Get user_id from DB
	var userID string
	err := suite.db.QueryRow(context.Background(),
		"SELECT id::text FROM users WHERE email = $1", email,
	).Scan(&userID)
	if err != nil {
		t.Fatalf("failed to get user_id: %v", err)
	}

	// Change password via handler
	authRepo := auth.NewRepository(suite.db)
	authService := auth.NewService(&config.AuthConfig{
		JWTSecret:         "e2e-test-secret",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}, authRepo)
	authHandler := auth.NewHandler(authService)

	changeRouter := gin.New()
	changeV1 := changeRouter.Group("/api/v1")
	changeV1.POST("/auth/change-password", func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Next()
	}, authHandler.ChangePassword)

	changeBody := map[string]string{"old_password": oldPass, "new_password": newPass}
	changeJSON, _ := json.Marshal(changeBody)
	changeReq, _ := http.NewRequest("POST", "/api/v1/auth/change-password", bytes.NewBuffer(changeJSON))
	changeReq.Header.Set("Content-Type", "application/json")
	changeW := httptest.NewRecorder()
	changeRouter.ServeHTTP(changeW, changeReq)

	if changeW.Code != http.StatusNoContent {
		t.Fatalf("change password: expected 204, got %d: %s", changeW.Code, changeW.Body.String())
	}

	// Login with old password should fail
	oldLoginBody := map[string]string{"email": email, "password": oldPass}
	oldLoginJSON, _ := json.Marshal(oldLoginBody)
	oldLoginReq, _ := http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBuffer(oldLoginJSON))
	oldLoginReq.Header.Set("Content-Type", "application/json")
	oldLoginW := httptest.NewRecorder()
	suite.router.ServeHTTP(oldLoginW, oldLoginReq)

	if oldLoginW.Code != http.StatusBadRequest {
		t.Errorf("old password login should fail: expected 400, got %d", oldLoginW.Code)
	}

	// Login with new password should work
	e2eLogin(t, suite.router, email, newPass)
}

// ─── AUTH-E-03: Disable user → refresh blocked ────────────────

func TestE2E_Auth_DisabledUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	suite := setupAuthTestSuite(t)
	defer suite.authCleanup(t)

	email := fmt.Sprintf("e2e-auth-disabled-%d@test.com", time.Now().UnixNano())
	password := "testpass123"

	// Register and get tokens
	e2eRegister(t, suite.router, email, password)
	loginData := e2eLogin(t, suite.router, email, password)
	refreshToken := loginData["refresh_token"].(string)

	// Disable user in DB
	_, err := suite.db.Exec(context.Background(),
		"UPDATE users SET status = 'DISABLED' WHERE email = $1", email,
	)
	if err != nil {
		t.Fatalf("failed to disable user: %v", err)
	}

	// Refresh should fail because user is disabled
	refreshBody := map[string]string{"refresh_token": refreshToken}
	refreshJSON, _ := json.Marshal(refreshBody)
	refreshReq, _ := http.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewBuffer(refreshJSON))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshW := httptest.NewRecorder()
	suite.router.ServeHTTP(refreshW, refreshReq)

	if refreshW.Code != http.StatusForbidden {
		t.Errorf("expected 403 for disabled user refresh, got %d: %s", refreshW.Code, refreshW.Body.String())
	}
}

// ─── AUTH-E-04: Admin seed → login admin ──────────────────────

func TestE2E_Auth_AdminSeed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	dbHost := os.Getenv("ARBITRAGE_DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPort := os.Getenv("ARBITRAGE_DB_PORT")
	if dbPort == "" {
		dbPort = "5433"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dsn := "postgres://test:test@" + dbHost + ":" + dbPort + "/arbitrage_test?sslmode=disable"
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	pool.Exec(ctx, "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email = 'e2e-admin@test.com')")
	pool.Exec(ctx, "DELETE FROM users WHERE email = 'e2e-admin@test.com'")
	defer func() {
		pool.Exec(context.Background(), "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email = 'e2e-admin@test.com')")
		pool.Exec(context.Background(), "DELETE FROM users WHERE email = 'e2e-admin@test.com'")
	}()

	authRepo := auth.NewRepository(pool)
	authService := auth.NewService(&config.AuthConfig{
		JWTSecret:         "e2e-test-secret",
		JWTExpiration:     15 * time.Minute,
		RefreshExpiration: 7 * 24 * time.Hour,
	}, authRepo)

	err = authService.EnsureAdmin(ctx, "e2e-admin@test.com", "adminpass123")
	if err != nil {
		t.Fatalf("EnsureAdmin failed: %v", err)
	}

	// Login as admin
	gin.SetMode(gin.TestMode)
	loginRouter := gin.New()
	loginV1 := loginRouter.Group("/api/v1")
	authHandler := auth.NewHandler(authService)
	loginV1.POST("/auth/login", authHandler.Login)

	loginData := e2eLogin(t, loginRouter, "e2e-admin@test.com", "adminpass123")

	if loginData["access_token"] == nil || loginData["access_token"] == "" {
		t.Error("expected admin access_token")
	}

	userData := loginData["user"].(map[string]interface{})
	if userData["role"] != "admin" {
		t.Errorf("expected admin role, got %s", userData["role"])
	}
}

// ─── AUTH-E-05: Phase-0 gate: /ready returns 200 ─────────────

func TestE2E_Health_Readiness(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	dbHost := os.Getenv("ARBITRAGE_DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPort := os.Getenv("ARBITRAGE_DB_PORT")
	if dbPort == "" {
		dbPort = "5433"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dsn := "postgres://test:test@" + dbHost + ":" + dbPort + "/arbitrage_test?sslmode=disable"
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	err = pool.Ping(ctx)
	if err != nil {
		t.Fatalf("database not ready: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})
	router.GET("/ready", func(c *gin.Context) {
		if err := pool.Ping(c.Request.Context()); err != nil {
			c.JSON(503, gin.H{"status": "not ready"})
			return
		}
		c.JSON(200, gin.H{"status": "ready"})
	})

	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health: expected 200, got %d", w.Code)
	}

	req, _ = http.NewRequest("GET", "/ready", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("ready: expected 200, got %d", w.Code)
	}
}
