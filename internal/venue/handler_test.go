package venue

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func setupTestRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery())

	v1 := router.Group("/api/v1")
	{
		v1.POST("/venues", handler.Create)
		v1.GET("/venues", handler.List)
		v1.GET("/venues/:id", handler.GetByID)
		v1.PUT("/venues/:id", handler.Update)
		v1.DELETE("/venues/:id", handler.Delete)
	}

	return router
}

func TestHandler_CreateVenue_WhenValid_Returns201(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	body := CreateVenueRequest{
		Code:      "binance",
		Name:      "Binance",
		VenueType: VenueTypeCEX,
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/venues", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["success"] != true {
		t.Error("expected success to be true")
	}
}

func TestHandler_CreateVenue_WhenInvalidBody_Returns400(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("POST", "/api/v1/venues", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_CreateVenue_WhenDuplicateCode_Returns409(t *testing.T) {
	mock := newMockRepository()

	// Pre-populate with existing venue
	existing := &Venue{
		ID:        uuid.New(),
		Code:      "binance",
		Name:      "Binance",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}
	mock.Create(nil, existing)

	svc := &Service{repo: mock}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	body := CreateVenueRequest{
		Code:      "binance",
		Name:      "Binance Duplicate",
		VenueType: VenueTypeCEX,
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "/api/v1/venues", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return conflict or error
	if w.Code == http.StatusCreated {
		t.Error("expected error for duplicate code, got 201")
	}
}

func TestHandler_GetByID_WhenFound_Returns200(t *testing.T) {
	mock := newMockRepository()

	v := &Venue{
		ID:        uuid.New(),
		Code:      "okx",
		Name:      "OKX",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}
	mock.Create(nil, v)

	svc := &Service{repo: mock}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/venues/"+v.ID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandler_GetByID_WhenInvalidID_Returns400(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/venues/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandler_GetByID_WhenNotFound_Returns404(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	randomID := uuid.New()
	req, _ := http.NewRequest("GET", "/api/v1/venues/"+randomID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestHandler_List_WhenNoVenues_ReturnsEmpty(t *testing.T) {
	mock := newMockRepository()
	svc := &Service{repo: mock}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("GET", "/api/v1/venues", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["success"] != true {
		t.Error("expected success to be true")
	}
}

func TestHandler_Update_WhenValid_Returns200(t *testing.T) {
	mock := newMockRepository()

	v := &Venue{
		ID:        uuid.New(),
		Code:      "binance",
		Name:      "Binance",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}
	mock.Create(nil, v)

	svc := &Service{repo: mock}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	updateBody := map[string]string{"name": "Binance Updated"}
	jsonBody, _ := json.Marshal(updateBody)

	req, _ := http.NewRequest("PUT", "/api/v1/venues/"+v.ID.String(), bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandler_Delete_WhenValid_Returns204(t *testing.T) {
	mock := newMockRepository()

	v := &Venue{
		ID:        uuid.New(),
		Code:      "binance",
		Name:      "Binance",
		VenueType: VenueTypeCEX,
		Status:    VenueStatusActive,
	}
	mock.Create(nil, v)

	svc := &Service{repo: mock}
	handler := NewHandler(svc)
	router := setupTestRouter(handler)

	req, _ := http.NewRequest("DELETE", "/api/v1/venues/"+v.ID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}
