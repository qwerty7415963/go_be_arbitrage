package walletgroup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

const (
	userA = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	userB = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

func setupTestRouter(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Test auth shim: X-Test-User provides the JWT user_id; a request
	// without the header simulates an unauthenticated (no-JWT) call.
	router.Use(func(c *gin.Context) {
		if uid := c.GetHeader("X-Test-User"); uid != "" {
			c.Set("user_id", uid)
		}
		c.Next()
	})

	handler.RegisterRoutes(router.Group("/api/v1"))
	return router
}

func doJSON(t *testing.T, router *gin.Engine, method, url, userID string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if userID != "" {
		req.Header.Set("X-Test-User", userID)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", w.Body.String(), err)
	}
	return resp
}

func errorFields(t *testing.T, w *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	resp := decodeBody(t, w)
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object, got %v", resp)
	}
	code, _ := errObj["code"].(string)
	msg, _ := errObj["message"].(string)
	return code, msg
}

func ownerGroup(id uuid.UUID) *Group {
	return &Group{ID: id, UserID: uuid.MustParse(userA), Name: "Smart Money"}
}

// ─── GRP-H-01: POST /groups valid → 201 ─────────────────────────

func TestHandler_Create_Valid_Returns201(t *testing.T) {
	repo := &mockRepo{
		createGroupFn: func(ctx context.Context, g *Group) error { return nil },
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "POST", "/api/v1/groups", userA, CreateGroupRequest{Name: "Smart Money"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	data := resp["data"].(map[string]interface{})
	if data["name"] != "Smart Money" {
		t.Errorf("expected group in response, got %v", data)
	}
	if resp["success"] != true {
		t.Error("expected success true")
	}
}

// ─── GRP-H-02: blank name → 400 COMMON-902 ──────────────────────

func TestHandler_Create_BlankName_Returns400(t *testing.T) {
	repo := &mockRepo{}
	router := setupTestRouter(NewHandler(NewService(repo)))

	for _, name := range []string{"", "   "} {
		w := doJSON(t, router, "POST", "/api/v1/groups", userA, CreateGroupRequest{Name: name})
		if w.Code != http.StatusBadRequest {
			t.Errorf("name %q: expected 400, got %d", name, w.Code)
			continue
		}
		code, _ := errorFields(t, w)
		if code != string(domain.ErrCodeValidation) {
			t.Errorf("name %q: expected COMMON-902, got %s", name, code)
		}
	}
}

// ─── GRP-H-03: duplicate name → 409 GROUP-002 ───────────────────

func TestHandler_Create_DuplicateName_Returns409(t *testing.T) {
	repo := &mockRepo{
		createGroupFn: func(ctx context.Context, g *Group) error { return ErrDuplicateName },
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "POST", "/api/v1/groups", userA, CreateGroupRequest{Name: "Smart Money"})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if code, _ := errorFields(t, w); code != string(domain.ErrCodeGroupDuplicate) {
		t.Errorf("expected GROUP-002, got %s", code)
	}
}

// ─── GRP-H-04: no JWT / no user context → 401 or 403 ────────────

func TestHandler_Create_NoAuth_Returns401or403(t *testing.T) {
	repo := &mockRepo{
		createGroupFn: func(ctx context.Context, g *Group) error { return nil },
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "POST", "/api/v1/groups", "", CreateGroupRequest{Name: "X"})
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Errorf("expected 401 or 403, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── GRP-H-05: list isolation ───────────────────────────────────

func TestHandler_List_OnlyOwnGroups(t *testing.T) {
	var listedFor string
	repo := &mockRepo{
		listGroupsFn: func(ctx context.Context, userID uuid.UUID) ([]*Group, error) {
			listedFor = userID.String()
			if userID.String() == userA {
				return []*Group{
					{ID: uuid.New(), UserID: userID, Name: "A-1"},
					{ID: uuid.New(), UserID: userID, Name: "A-2"},
				}, nil
			}
			return []*Group{}, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "GET", "/api/v1/groups", userA, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if listedFor != userA {
		t.Errorf("expected query for user A, got %s", listedFor)
	}
	resp := decodeBody(t, w)
	items := resp["data"].([]interface{})
	if len(items) != 2 {
		t.Errorf("expected 2 groups for A, got %d", len(items))
	}
}

// ─── GRP-H-06: get own group → 200 + wallet_count ───────────────

func TestHandler_Get_OwnGroup_Returns200WithCount(t *testing.T) {
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			g := ownerGroup(id)
			g.WalletCount = 4
			return g, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "GET", "/api/v1/groups/"+groupID.String(), userA, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	data := decodeBody(t, w)["data"].(map[string]interface{})
	if data["wallet_count"] != float64(4) {
		t.Errorf("expected wallet_count 4, got %v", data["wallet_count"])
	}
}

// ─── GRP-H-07: foreign group → 403 GROUP-003 ────────────────────

func TestHandler_Get_ForeignGroup_Returns403(t *testing.T) {
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: id, UserID: uuid.MustParse(userA), Name: "A's"}, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "GET", "/api/v1/groups/"+groupID.String(), userB, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if code, _ := errorFields(t, w); code != string(domain.ErrCodeGroupForbidden) {
		t.Errorf("expected GROUP-003, got %s", code)
	}
}

// ─── GRP-H-08: unknown id → 404 GROUP-001 ───────────────────────

func TestHandler_Get_UnknownID_Returns404(t *testing.T) {
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return nil, pgx.ErrNoRows
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "GET", "/api/v1/groups/"+uuid.New().String(), userA, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if code, _ := errorFields(t, w); code != string(domain.ErrCodeGroupNotFound) {
		t.Errorf("expected GROUP-001, got %s", code)
	}
}

// ─── GRP-H-09: PATCH own group → 200 ────────────────────────────

func TestHandler_Update_OwnGroup_Returns200(t *testing.T) {
	groupID := uuid.New()
	newName := "Renamed"
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return ownerGroup(id), nil
		},
		updateGroupFn: func(ctx context.Context, g *Group) error { return nil },
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "PATCH", "/api/v1/groups/"+groupID.String(), userA,
		UpdateGroupRequest{Name: &newName})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	data := decodeBody(t, w)["data"].(map[string]interface{})
	if data["name"] != "Renamed" {
		t.Errorf("expected updated name, got %v", data["name"])
	}
}

// ─── GRP-H-10: PATCH cross-user → 403 ───────────────────────────

func TestHandler_Update_CrossUser_Returns403(t *testing.T) {
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: id, UserID: uuid.MustParse(userA), Name: "A's"}, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "PATCH", "/api/v1/groups/"+groupID.String(), userB,
		UpdateGroupRequest{Name: strPtr("Hacked")})
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── GRP-H-11: DELETE own → 204 ─────────────────────────────────

func TestHandler_Delete_OwnGroup_Returns204(t *testing.T) {
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return ownerGroup(id), nil
		},
		deleteGroupFn: func(ctx context.Context, id, userID uuid.UUID) (int64, error) {
			return 1, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "DELETE", "/api/v1/groups/"+groupID.String(), userA, nil)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── GRP-H-12: delete twice → 404 on second ─────────────────────

func TestHandler_Delete_Twice_Returns404Second(t *testing.T) {
	groupID := uuid.New()
	deleted := false
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			if deleted {
				return nil, pgx.ErrNoRows
			}
			return ownerGroup(id), nil
		},
		deleteGroupFn: func(ctx context.Context, id, userID uuid.UUID) (int64, error) {
			deleted = true
			return 1, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "DELETE", "/api/v1/groups/"+groupID.String(), userA, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("first delete: expected 204, got %d", w.Code)
	}
	w = doJSON(t, router, "DELETE", "/api/v1/groups/"+groupID.String(), userA, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("second delete: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── GRP-H-13: POST single wallet → 200 ─────────────────────────

func TestHandler_AddWallets_Single_Returns200(t *testing.T) {
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return ownerGroup(id), nil
		},
		addMembersFn: func(ctx context.Context, g, by uuid.UUID, items []walletItem) (int64, error) {
			return 1, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "POST", "/api/v1/groups/"+groupID.String()+"/wallets", userA,
		WalletsRequest{Wallets: []string{uuid.New().String()}})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	data := decodeBody(t, w)["data"].(map[string]interface{})
	if data["added"] != float64(1) {
		t.Errorf("expected added 1, got %v", data["added"])
	}
}

// ─── GRP-H-14: bulk 10 wallets → 200, 10 memberships ────────────

func TestHandler_AddWallets_Bulk10_Returns200(t *testing.T) {
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return ownerGroup(id), nil
		},
		addMembersFn: func(ctx context.Context, g, by uuid.UUID, items []walletItem) (int64, error) {
			if len(items) != 10 {
				t.Errorf("expected 10 items, got %d", len(items))
			}
			return 10, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	entries := make([]string, 10)
	for i := range entries {
		entries[i] = uuid.New().String()
	}
	w := doJSON(t, router, "POST", "/api/v1/groups/"+groupID.String()+"/wallets", userA,
		WalletsRequest{Wallets: entries})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	data := decodeBody(t, w)["data"].(map[string]interface{})
	if data["added"] != float64(10) {
		t.Errorf("expected added 10, got %v", data["added"])
	}
}

// ─── GRP-H-15: duplicate wallet (body + existing) → 200 ─────────

func TestHandler_AddWallets_Duplicate_Idempotent200(t *testing.T) {
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return ownerGroup(id), nil
		},
		addMembersFn: func(ctx context.Context, g, by uuid.UUID, items []walletItem) (int64, error) {
			return 0, nil // ON CONFLICT DO NOTHING
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	wallet := uuid.New().String()
	w := doJSON(t, router, "POST", "/api/v1/groups/"+groupID.String()+"/wallets", userA,
		WalletsRequest{Wallets: []string{wallet, wallet}})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	data := decodeBody(t, w)["data"].(map[string]interface{})
	if data["added"] != float64(0) {
		t.Errorf("expected added 0, got %v", data["added"])
	}
}

// ─── GRP-H-16: unknown wallet → 404 WALLET-001 ──────────────────

func TestHandler_AddWallets_UnknownWallet_Returns404(t *testing.T) {
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return ownerGroup(id), nil
		},
		addMembersFn: func(ctx context.Context, g, by uuid.UUID, items []walletItem) (int64, error) {
			return 0, ErrWalletNotFound
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "POST", "/api/v1/groups/"+groupID.String()+"/wallets", userA,
		WalletsRequest{Wallets: []string{uuid.New().String()}})
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if code, _ := errorFields(t, w); code != string(domain.ErrCodeWalletNotFound) {
		t.Errorf("expected WALLET-001, got %s", code)
	}
}

// ─── GRP-H-17: add to cross-user group → 403 ────────────────────

func TestHandler_AddWallets_CrossUser_Returns403(t *testing.T) {
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: id, UserID: uuid.MustParse(userA), Name: "A's"}, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "POST", "/api/v1/groups/"+groupID.String()+"/wallets", userB,
		WalletsRequest{Wallets: []string{uuid.New().String()}})
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── GRP-H-18: DELETE remove 1 of 3 → 204 ───────────────────────

func TestHandler_RemoveWallets_Returns204(t *testing.T) {
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return ownerGroup(id), nil
		},
		removeMembersFn: func(ctx context.Context, g uuid.UUID, items []walletItem) (int64, error) {
			if len(items) != 1 {
				t.Errorf("expected 1 item, got %d", len(items))
			}
			return 1, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "DELETE", "/api/v1/groups/"+groupID.String()+"/wallets", userA,
		WalletsRequest{Wallets: []string{uuid.New().String()}})
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── GRP-H-19: GET wallets search=0xabc → matched only ──────────

func TestHandler_ListWallets_SearchPartial(t *testing.T) {
	groupID := uuid.New()
	var gotSearch string
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return ownerGroup(id), nil
		},
		listMembersFn: func(ctx context.Context, g uuid.UUID, search string, limit, offset int) ([]*WalletRef, int64, error) {
			gotSearch = search
			return []*WalletRef{{ID: uuid.New(), Chain: "evm", Address: "0xabc123"}}, 1, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "GET", "/api/v1/groups/"+groupID.String()+"/wallets?search=0xabc", userA, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotSearch != "0xabc" {
		t.Errorf("expected search 0xabc, got %q", gotSearch)
	}
	items := decodeBody(t, w)["data"].([]interface{})
	if len(items) != 1 {
		t.Errorf("expected 1 wallet, got %d", len(items))
	}
}

// ─── GRP-H-20: page=2&limit=5 → stable offset pagination ────────

func TestHandler_ListWallets_Pagination(t *testing.T) {
	groupID := uuid.New()
	var gotLimit, gotOffset int
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return ownerGroup(id), nil
		},
		listMembersFn: func(ctx context.Context, g uuid.UUID, search string, limit, offset int) ([]*WalletRef, int64, error) {
			gotLimit, gotOffset = limit, offset
			return []*WalletRef{}, 12, nil
		},
	}
	router := setupTestRouter(NewHandler(NewService(repo)))

	w := doJSON(t, router, "GET", fmt.Sprintf("/api/v1/groups/%s/wallets?page=2&limit=5", groupID), userA, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotLimit != 5 || gotOffset != 5 {
		t.Errorf("expected limit=5 offset=5, got limit=%d offset=%d", gotLimit, gotOffset)
	}
	resp := decodeBody(t, w)
	meta := resp["meta"].(map[string]interface{})
	if meta["page"] != float64(2) || meta["total_pages"] != float64(3) {
		t.Errorf("expected meta page=2 total_pages=3, got %v", meta)
	}
}

func strPtr(s string) *string { return &s }
