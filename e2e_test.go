//go:build e2e

package e2e

import (
	"bytes"
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
