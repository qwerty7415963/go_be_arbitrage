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
	createFn      func(ctx context.Context, userID uuid.UUID, name, desc string) (*Group, error)
	listFn        func(ctx context.Context, userID uuid.UUID) ([]*Group, error)
	getFn         func(ctx context.Context, gid, uid uuid.UUID) (*Group, error)
	updateFn      func(ctx context.Context, gid, uid uuid.UUID, name, desc *string) (*Group, error)
	deleteFn      func(ctx context.Context, gid, uid uuid.UUID) error
	addFn         func(ctx context.Context, gid, uid uuid.UUID, items []MemberInput) (int64, error)
	removeFn      func(ctx context.Context, gid, uid, venueID uuid.UUID, addrs []string) (int64, error)
	listMembersFn func(ctx context.Context, gid, uid uuid.UUID) ([]*Member, error)
	ownerFn       func(ctx context.Context, gid uuid.UUID) (uuid.UUID, error)
	venueFn       func(ctx context.Context, code string) (uuid.UUID, error)
}

func (m *mockStore) Create(ctx context.Context, u uuid.UUID, n, d string) (*Group, error) {
	return m.createFn(ctx, u, n, d)
}
func (m *mockStore) List(ctx context.Context, u uuid.UUID) ([]*Group, error) {
	return m.listFn(ctx, u)
}
func (m *mockStore) Get(ctx context.Context, g, u uuid.UUID) (*Group, error) {
	return m.getFn(ctx, g, u)
}
func (m *mockStore) Update(ctx context.Context, g, u uuid.UUID, n, d *string) (*Group, error) {
	return m.updateFn(ctx, g, u, n, d)
}
func (m *mockStore) Delete(ctx context.Context, g, u uuid.UUID) error {
	return m.deleteFn(ctx, g, u)
}
func (m *mockStore) AddMembers(ctx context.Context, g, u uuid.UUID, it []MemberInput) (int64, error) {
	return m.addFn(ctx, g, u, it)
}
func (m *mockStore) RemoveMembers(ctx context.Context, g, u, v uuid.UUID, a []string) (int64, error) {
	return m.removeFn(ctx, g, u, v, a)
}
func (m *mockStore) ListMembers(ctx context.Context, g, u uuid.UUID) ([]*Member, error) {
	return m.listMembersFn(ctx, g, u)
}
func (m *mockStore) OwnerOf(ctx context.Context, g uuid.UUID) (uuid.UUID, error) {
	return m.ownerFn(ctx, g)
}
func (m *mockStore) VenueIDByCode(ctx context.Context, c string) (uuid.UUID, error) {
	return m.venueFn(ctx, c)
}

const authedUserID = "11111111-1111-1111-1111-111111111111"

func testRouter(h *Handler, withUser bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
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
		"POST": "/api/v1/trader-groups", "GET": "/api/v1/trader-groups",
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
	w := doReq(t, testRouter(h, true), "POST", "/api/v1/trader-groups", `{"name":"Alphas"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}

	h2 := NewHandler(&mockStore{})
	w2 := doReq(t, testRouter(h2, true), "POST", "/api/v1/trader-groups", `{"name":""}`)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("blank name: got %d", w2.Code)
	}

	h3 := NewHandler(&mockStore{createFn: func(context.Context, uuid.UUID, string, string) (*Group, error) {
		return nil, domain.NewError(domain.ErrCodeGroupDuplicate, "dup")
	}})
	w3 := doReq(t, testRouter(h3, true), "POST", "/api/v1/trader-groups", `{"name":"Alphas"}`)
	if w3.Code != http.StatusConflict || errCode(t, w3) != "GROUP-002" {
		t.Errorf("dup: got %d %s", w3.Code, w3.Body.String())
	}
}

// Get: bad UUID 400, unknown 404, foreign 403.
func TestHandler_Get_Errors(t *testing.T) {
	h := NewHandler(&mockStore{})
	w := doReq(t, testRouter(h, true), "GET", "/api/v1/trader-groups/nope", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad uuid: got %d", w.Code)
	}
	id := uuid.NewString()
	h2 := NewHandler(&mockStore{getFn: func(context.Context, uuid.UUID, uuid.UUID) (*Group, error) {
		return nil, domain.NewError(domain.ErrCodeNotFound, "nope")
	}})
	if w := doReq(t, testRouter(h2, true), "GET", "/api/v1/trader-groups/"+id, ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown: got %d", w.Code)
	}
	h3 := NewHandler(&mockStore{getFn: func(context.Context, uuid.UUID, uuid.UUID) (*Group, error) {
		return nil, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	if w := doReq(t, testRouter(h3, true), "GET", "/api/v1/trader-groups/"+id, ""); w.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w.Code)
	}
}

// AddMembers: bad address 400 before reaching the store.
func TestHandler_AddMembers_Validation(t *testing.T) {
	called := false
	h := NewHandler(&mockStore{addFn: func(context.Context, uuid.UUID, uuid.UUID, []MemberInput) (int64, error) {
		called = true
		return 0, nil
	}})
	w := doReq(t, testRouter(h, true), "POST", "/api/v1/trader-groups/"+uuid.NewString()+"/members",
		`{"members":[{"venue":"hyperliquid","wallet_address":"zzz"}]}`)
	if w.Code != http.StatusBadRequest || called {
		t.Errorf("bad address: got %d called=%v", w.Code, called)
	}
}

// Delete: 204, unknown 404.
func TestHandler_Delete(t *testing.T) {
	h := NewHandler(&mockStore{deleteFn: func(context.Context, uuid.UUID, uuid.UUID) error { return nil }})
	w := doReq(t, testRouter(h, true), "DELETE", "/api/v1/trader-groups/"+uuid.NewString(), "")
	if w.Code != http.StatusNoContent {
		t.Errorf("got %d", w.Code)
	}
	h2 := NewHandler(&mockStore{deleteFn: func(context.Context, uuid.UUID, uuid.UUID) error {
		return errors.New("db gone")
	}})
	w2 := doReq(t, testRouter(h2, true), "DELETE", "/api/v1/trader-groups/"+uuid.NewString(), "")
	if w2.Code != http.StatusInternalServerError {
		t.Errorf("raw error must map to 500, got %d", w2.Code)
	}
}
