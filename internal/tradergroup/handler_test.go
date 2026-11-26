package tradergroup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type mockStore struct {
	createFn          func(ctx context.Context, userID uuid.UUID, name, desc string) (*Group, error)
	listFn            func(ctx context.Context, userID uuid.UUID, params ListGroupsParams) ([]*GroupListItem, int, error)
	getFn             func(ctx context.Context, gid, uid uuid.UUID) (*Group, error)
	updateFn          func(ctx context.Context, gid, uid uuid.UUID, name, desc *string, isFav, notify *bool) (*Group, error)
	deleteFn          func(ctx context.Context, gid, uid uuid.UUID) error
	addFn             func(ctx context.Context, gid, uid uuid.UUID, items []MemberInput) (int64, int64, error)
	removeFn          func(ctx context.Context, gid, uid, venueID uuid.UUID, addrs []string) (int64, error)
	updateMembersFn   func(ctx context.Context, gid, uid uuid.UUID, items []ValidatedMemberUpdate) (int64, error)
	listMembersFn     func(ctx context.Context, gid, uid uuid.UUID, params ListMembersParams) ([]*MemberRow, int, error)
	activityFn        func(ctx context.Context, gid, uid uuid.UUID, f ActivityFilter) (*ActivityPage, error)
	ownerFn           func(ctx context.Context, gid uuid.UUID) (uuid.UUID, error)
	venueFn           func(ctx context.Context, code string) (uuid.UUID, error)
	overviewFn        func(ctx context.Context, userID uuid.UUID, period string) (*OverviewStats, error)
	toggleFavoriteFn  func(ctx context.Context, gid, uid uuid.UUID, isFavorite bool) (*Group, error)
	duplicateFn       func(ctx context.Context, gid, uid uuid.UUID, name *string) (*Group, error)
	listAlertsFn      func(ctx context.Context, gid, uid uuid.UUID) ([]*GroupAlert, error)
	createAlertFn     func(ctx context.Context, gid, uid uuid.UUID, req CreateAlertRequest) (*GroupAlert, error)
	updateAlertFn     func(ctx context.Context, aid, uid uuid.UUID, req UpdateAlertRequest) (*GroupAlert, error)
	deleteAlertFn     func(ctx context.Context, aid, uid uuid.UUID) error
	listAlertEventsFn func(ctx context.Context, gid, uid uuid.UUID, unread bool, limit int, cursor string) (*AlertEventPage, error)
	markAllReadFn     func(ctx context.Context, gid, uid uuid.UUID) (int, error)
	unreadCountFn     func(ctx context.Context, uid uuid.UUID) (int, map[uuid.UUID]int, error)
	markEventReadFn   func(ctx context.Context, eid, uid uuid.UUID) error
	statsFn           func(ctx context.Context, gid, uid uuid.UUID, period string) (*GroupStats, error)
	positionsFn       func(ctx context.Context, gid, uid uuid.UUID) (*PositionsResponse, error)
	exposureFn        func(ctx context.Context, gid, uid uuid.UUID, period string) (*ExposureResponse, error)
	performanceFn     func(ctx context.Context, gid, uid uuid.UUID, period string) (*PerformanceResponse, error)
	compareFn         func(ctx context.Context, uid uuid.UUID, gids []uuid.UUID, period string) (*CompareResponse, error)
}

func (m *mockStore) Create(ctx context.Context, u uuid.UUID, n, d string) (*Group, error) {
	if m.createFn == nil {
		return nil, nil
	}
	return m.createFn(ctx, u, n, d)
}
func (m *mockStore) List(ctx context.Context, u uuid.UUID, params ListGroupsParams) ([]*GroupListItem, int, error) {
	if m.listFn == nil {
		return []*GroupListItem{}, 0, nil
	}
	return m.listFn(ctx, u, params)
}
func (m *mockStore) Get(ctx context.Context, g, u uuid.UUID) (*Group, error) {
	if m.getFn == nil {
		return nil, nil
	}
	return m.getFn(ctx, g, u)
}
func (m *mockStore) Update(ctx context.Context, g, u uuid.UUID, n, d *string, isFav, notify *bool) (*Group, error) {
	if m.updateFn == nil {
		return nil, nil
	}
	return m.updateFn(ctx, g, u, n, d, isFav, notify)
}
func (m *mockStore) Delete(ctx context.Context, g, u uuid.UUID) error {
	if m.deleteFn == nil {
		return nil
	}
	return m.deleteFn(ctx, g, u)
}
func (m *mockStore) AddMembers(ctx context.Context, g, u uuid.UUID, it []MemberInput) (int64, int64, error) {
	if m.addFn == nil {
		return 0, 0, nil
	}
	return m.addFn(ctx, g, u, it)
}
func (m *mockStore) RemoveMembers(ctx context.Context, g, u, v uuid.UUID, a []string) (int64, error) {
	if m.removeFn == nil {
		return 0, nil
	}
	return m.removeFn(ctx, g, u, v, a)
}
func (m *mockStore) UpdateMembers(ctx context.Context, g, u uuid.UUID, it []ValidatedMemberUpdate) (int64, error) {
	if m.updateMembersFn == nil {
		return 0, nil
	}
	return m.updateMembersFn(ctx, g, u, it)
}
func (m *mockStore) ListMembers(ctx context.Context, g, u uuid.UUID, params ListMembersParams) ([]*MemberRow, int, error) {
	if m.listMembersFn == nil {
		return []*MemberRow{}, 0, nil
	}
	return m.listMembersFn(ctx, g, u, params)
}
func (m *mockStore) OwnerOf(ctx context.Context, g uuid.UUID) (uuid.UUID, error) {
	if m.ownerFn == nil {
		return uuid.Nil, nil
	}
	return m.ownerFn(ctx, g)
}
func (m *mockStore) VenueIDByCode(ctx context.Context, c string) (uuid.UUID, error) {
	if m.venueFn == nil {
		return uuid.Nil, nil
	}
	return m.venueFn(ctx, c)
}
func (m *mockStore) Overview(ctx context.Context, userID uuid.UUID, period string) (*OverviewStats, error) {
	if m.overviewFn == nil {
		return nil, nil
	}
	return m.overviewFn(ctx, userID, period)
}
func (m *mockStore) ToggleFavorite(ctx context.Context, g, u uuid.UUID, isFavorite bool) (*Group, error) {
	if m.toggleFavoriteFn == nil {
		return nil, nil
	}
	return m.toggleFavoriteFn(ctx, g, u, isFavorite)
}
func (m *mockStore) Duplicate(ctx context.Context, g, u uuid.UUID, name *string) (*Group, error) {
	if m.duplicateFn == nil {
		return nil, nil
	}
	return m.duplicateFn(ctx, g, u, name)
}
func (m *mockStore) Activity(ctx context.Context, g, u uuid.UUID, f ActivityFilter) (*ActivityPage, error) {
	if m.activityFn == nil {
		return nil, nil
	}
	return m.activityFn(ctx, g, u, f)
}

func (m *mockStore) Stats(ctx context.Context, g, u uuid.UUID, period string) (*GroupStats, error) {
	if m.statsFn == nil {
		return &GroupStats{}, nil
	}
	return m.statsFn(ctx, g, u, period)
}
func (m *mockStore) Positions(ctx context.Context, g, u uuid.UUID) (*PositionsResponse, error) {
	if m.positionsFn == nil {
		return &PositionsResponse{}, nil
	}
	return m.positionsFn(ctx, g, u)
}
func (m *mockStore) Exposure(ctx context.Context, g, u uuid.UUID, period string) (*ExposureResponse, error) {
	if m.exposureFn == nil {
		return &ExposureResponse{}, nil
	}
	return m.exposureFn(ctx, g, u, period)
}
func (m *mockStore) Performance(ctx context.Context, g, u uuid.UUID, period string) (*PerformanceResponse, error) {
	if m.performanceFn == nil {
		return &PerformanceResponse{}, nil
	}
	return m.performanceFn(ctx, g, u, period)
}
func (m *mockStore) Compare(ctx context.Context, u uuid.UUID, gids []uuid.UUID, period string) (*CompareResponse, error) {
	if m.compareFn == nil {
		return &CompareResponse{}, nil
	}
	return m.compareFn(ctx, u, gids, period)
}

func (m *mockStore) ListAlerts(ctx context.Context, g, u uuid.UUID) ([]*GroupAlert, error) {
	if m.listAlertsFn == nil {
		return []*GroupAlert{}, nil
	}
	return m.listAlertsFn(ctx, g, u)
}
func (m *mockStore) CreateAlert(ctx context.Context, g, u uuid.UUID, req CreateAlertRequest) (*GroupAlert, error) {
	if m.createAlertFn == nil {
		return nil, nil
	}
	return m.createAlertFn(ctx, g, u, req)
}
func (m *mockStore) UpdateAlert(ctx context.Context, a, u uuid.UUID, req UpdateAlertRequest) (*GroupAlert, error) {
	if m.updateAlertFn == nil {
		return nil, nil
	}
	return m.updateAlertFn(ctx, a, u, req)
}
func (m *mockStore) DeleteAlert(ctx context.Context, a, u uuid.UUID) error {
	if m.deleteAlertFn == nil {
		return nil
	}
	return m.deleteAlertFn(ctx, a, u)
}
func (m *mockStore) ListAlertEvents(ctx context.Context, g, u uuid.UUID, unread bool, limit int, cursor string) (*AlertEventPage, error) {
	if m.listAlertEventsFn == nil {
		return &AlertEventPage{Items: []AlertEvent{}}, nil
	}
	return m.listAlertEventsFn(ctx, g, u, unread, limit, cursor)
}
func (m *mockStore) MarkAllAlertEventsRead(ctx context.Context, g, u uuid.UUID) (int, error) {
	if m.markAllReadFn == nil {
		return 0, nil
	}
	return m.markAllReadFn(ctx, g, u)
}
func (m *mockStore) UnreadAlertCount(ctx context.Context, u uuid.UUID) (int, map[uuid.UUID]int, error) {
	if m.unreadCountFn == nil {
		return 0, map[uuid.UUID]int{}, nil
	}
	return m.unreadCountFn(ctx, u)
}
func (m *mockStore) MarkAlertEventRead(ctx context.Context, e, u uuid.UUID) error {
	if m.markEventReadFn == nil {
		return nil
	}
	return m.markEventReadFn(ctx, e, u)
}

const authedUserID = "11111111-1111-1111-1111-111111111111"

func testRouter(h *Handler, withUser bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/v1")
	if withUser {
		v1.Use(func(c *gin.Context) {
			c.Set("user_id", authedUserID)
			c.Next()
		})
	}
	h.RegisterRoutes(v1)
	return r
}

func doReq(t *testing.T, r *gin.Engine, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := http.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp["error"].(map[string]any)["code"].(string)
}

// AUTH: anonymous group calls are 403 AUTH-005.
func TestHandler_Anonymous_Forbidden(t *testing.T) {
	h := NewHandler(&mockStore{})
	for method, target := range map[string]string{
		"POST": "/v1/trader-groups", "GET": "/v1/trader-groups",
	} {
		w := doReq(t, testRouter(h, false), method, target, `{"name":"x"}`)
		if w.Code != http.StatusForbidden || errCode(t, w) != "AUTH-005" {
			t.Errorf("%s: got %d %s", method, w.Code, w.Body.String())
		}
	}
}

// Create: 201 shape, 400 validation, 409 duplicate mapping.
func TestHandler_Create(t *testing.T) {
	h := NewHandler(&mockStore{createFn: func(_ context.Context, u uuid.UUID, n, d string) (*Group, error) {
		if u.String() != authedUserID || n != "Alphas" {
			t.Errorf("args: %v %q", u, n)
		}
		return &Group{ID: uuid.New(), Name: n}, nil
	}})
	w := doReq(t, testRouter(h, true), "POST", "/v1/trader-groups", `{"name":"Alphas"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}

	h2 := NewHandler(&mockStore{})
	w2 := doReq(t, testRouter(h2, true), "POST", "/v1/trader-groups", `{"name":""}`)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("blank name: got %d", w2.Code)
	}

	h3 := NewHandler(&mockStore{createFn: func(context.Context, uuid.UUID, string, string) (*Group, error) {
		return nil, domain.NewError(domain.ErrCodeGroupDuplicate, "dup")
	}})
	w3 := doReq(t, testRouter(h3, true), "POST", "/v1/trader-groups", `{"name":"Alphas"}`)
	if w3.Code != http.StatusConflict || errCode(t, w3) != "GROUP-002" {
		t.Errorf("dup: got %d %s", w3.Code, w3.Body.String())
	}
}

// Get: bad UUID 400, unknown 404, foreign 403.
func TestHandler_Get_Errors(t *testing.T) {
	h := NewHandler(&mockStore{})
	w := doReq(t, testRouter(h, true), "GET", "/v1/trader-groups/nope", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad uuid: got %d", w.Code)
	}
	id := uuid.NewString()
	h2 := NewHandler(&mockStore{getFn: func(context.Context, uuid.UUID, uuid.UUID) (*Group, error) {
		return nil, domain.NewError(domain.ErrCodeNotFound, "nope")
	}})
	if w := doReq(t, testRouter(h2, true), "GET", "/v1/trader-groups/"+id, ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown: got %d", w.Code)
	}
	h3 := NewHandler(&mockStore{getFn: func(context.Context, uuid.UUID, uuid.UUID) (*Group, error) {
		return nil, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	if w := doReq(t, testRouter(h3, true), "GET", "/v1/trader-groups/"+id, ""); w.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w.Code)
	}
}

// AddMembers: bad address 400 before reaching the store.
func TestHandler_AddMembers_Validation(t *testing.T) {
	called := false
	h := NewHandler(&mockStore{addFn: func(context.Context, uuid.UUID, uuid.UUID, []MemberInput) (int64, int64, error) {
		called = true
		return 0, 0, nil
	}})
	w := doReq(t, testRouter(h, true), "POST", "/v1/trader-groups/"+uuid.NewString()+"/members",
		`{"members":[{"venue":"hyperliquid","wallet_address":"zzz"}]}`)
	if w.Code != http.StatusBadRequest || called {
		t.Errorf("bad address: got %d called=%v", w.Code, called)
	}
}

// Delete: 204, unknown 404.
func TestHandler_Delete(t *testing.T) {
	h := NewHandler(&mockStore{deleteFn: func(context.Context, uuid.UUID, uuid.UUID) error { return nil }})
	w := doReq(t, testRouter(h, true), "DELETE", "/v1/trader-groups/"+uuid.NewString(), "")
	if w.Code != http.StatusNoContent {
		t.Errorf("got %d", w.Code)
	}
	h2 := NewHandler(&mockStore{deleteFn: func(context.Context, uuid.UUID, uuid.UUID) error {
		return errors.New("db gone")
	}})
	w2 := doReq(t, testRouter(h2, true), "DELETE", "/v1/trader-groups/"+uuid.NewString(), "")
	if w2.Code != http.StatusInternalServerError {
		t.Errorf("raw error must map to 500, got %d", w2.Code)
	}
}

// GRP-M-02: PATCH members — {updated} shape, validation, auth, isolation.
func TestHandler_UpdateMembers(t *testing.T) {
	h := NewHandler(&mockStore{updateMembersFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID, items []ValidatedMemberUpdate) (int64, error) {
		if len(items) != 1 || !items[0].SetAlias || items[0].Alias != "w1" || items[0].SetNote {
			t.Errorf("tri-state items: %+v", items)
		}
		if items[0].Venue != "hyperliquid" {
			t.Errorf("venue normalized: %+v", items[0])
		}
		return 1, nil
	}})
	w := doReq(t, testRouter(h, true), "PATCH", "/v1/trader-groups/"+uuid.NewString()+"/members",
		`{"members":[{"venue":"Hyperliquid","wallet_address":"0xABCDEF0123456789abcdef0123456789ABCDEF01","alias":"w1"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["data"].(map[string]any)["updated"] != float64(1) {
		t.Errorf("shape: %v", resp)
	}

	// Anonymous → 403.
	h2 := NewHandler(&mockStore{})
	w2 := doReq(t, testRouter(h2, false), "PATCH", "/v1/trader-groups/"+uuid.NewString()+"/members",
		`{"members":[]}`)
	if w2.Code != http.StatusForbidden {
		t.Errorf("anonymous: got %d", w2.Code)
	}

	// Over-cap alias → 400, store untouched.
	called := false
	long := strings.Repeat("x", 101)
	h3 := NewHandler(&mockStore{updateMembersFn: func(context.Context, uuid.UUID, uuid.UUID, []ValidatedMemberUpdate) (int64, error) {
		called = true
		return 0, nil
	}})
	w3 := doReq(t, testRouter(h3, true), "PATCH", "/v1/trader-groups/"+uuid.NewString()+"/members",
		`{"members":[{"venue":"hyperliquid","wallet_address":"0xabcdef0123456789abcdef0123456789abcdef01","alias":"`+long+`"}]}`)
	if w3.Code != http.StatusBadRequest || called {
		t.Errorf("cap: got %d called=%v", w3.Code, called)
	}

	// Foreign group → 403 via store.
	h4 := NewHandler(&mockStore{updateMembersFn: func(context.Context, uuid.UUID, uuid.UUID, []ValidatedMemberUpdate) (int64, error) {
		return 0, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	w4 := doReq(t, testRouter(h4, true), "PATCH", "/v1/trader-groups/"+uuid.NewString()+"/members",
		`{"members":[{"venue":"hyperliquid","wallet_address":"0xabcdef0123456789abcdef0123456789abcdef01"}]}`)
	if w4.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w4.Code)
	}
}
