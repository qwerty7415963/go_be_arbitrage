package tradergroup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

func TestHandler_Update_Extra(t *testing.T) {
	id := uuid.New()
	h := NewHandler(&mockStore{updateFn: func(_ context.Context, g, u uuid.UUID, n, d *string, isFav, notify *bool) (*Group, error) {
		return &Group{ID: g, Name: "New"}, nil
	}})
	w := doReq(t, testRouter(h, true), "PATCH", "/v1/trader-groups/"+id.String(), `{"name":"New"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	// validation
	h2 := NewHandler(&mockStore{})
	w2 := doReq(t, testRouter(h2, true), "PATCH", "/v1/trader-groups/"+id.String(), `{}`)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("empty update: got %d", w2.Code)
	}
	// foreign
	h3 := NewHandler(&mockStore{updateFn: func(context.Context, uuid.UUID, uuid.UUID, *string, *string, *bool, *bool) (*Group, error) {
		return nil, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	w3 := doReq(t, testRouter(h3, true), "PATCH", "/v1/trader-groups/"+id.String(), `{"name":"X"}`)
	if w3.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w3.Code)
	}
	// unknown
	h4 := NewHandler(&mockStore{updateFn: func(context.Context, uuid.UUID, uuid.UUID, *string, *string, *bool, *bool) (*Group, error) {
		return nil, domain.NewError(domain.ErrCodeNotFound, "nope")
	}})
	w4 := doReq(t, testRouter(h4, true), "PATCH", "/v1/trader-groups/"+id.String(), `{"name":"X"}`)
	if w4.Code != http.StatusNotFound {
		t.Errorf("unknown: got %d", w4.Code)
	}
	// anonymous
	h5 := NewHandler(&mockStore{})
	w5 := doReq(t, testRouter(h5, false), "PATCH", "/v1/trader-groups/"+id.String(), `{"name":"X"}`)
	if w5.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w5.Code)
	}
}

func TestHandler_Delete_Extra(t *testing.T) {
	id := uuid.New()
	// unknown
	h := NewHandler(&mockStore{deleteFn: func(context.Context, uuid.UUID, uuid.UUID) error {
		return domain.NewError(domain.ErrCodeNotFound, "nope")
	}})
	w := doReq(t, testRouter(h, true), "DELETE", "/v1/trader-groups/"+id.String(), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown: got %d", w.Code)
	}
	// foreign
	h2 := NewHandler(&mockStore{deleteFn: func(context.Context, uuid.UUID, uuid.UUID) error {
		return domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	w2 := doReq(t, testRouter(h2, true), "DELETE", "/v1/trader-groups/"+id.String(), "")
	if w2.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w2.Code)
	}
	// anonymous
	h3 := NewHandler(&mockStore{})
	w3 := doReq(t, testRouter(h3, false), "DELETE", "/v1/trader-groups/"+id.String(), "")
	if w3.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w3.Code)
	}
	// bad uuid
	h4 := NewHandler(&mockStore{})
	w4 := doReq(t, testRouter(h4, true), "DELETE", "/v1/trader-groups/nope", "")
	if w4.Code != http.StatusBadRequest {
		t.Errorf("bad uuid: got %d", w4.Code)
	}
}

func TestHandler_AddMembers_Extra(t *testing.T) {
	id := uuid.New()
	h := NewHandler(&mockStore{addFn: func(_ context.Context, g, u uuid.UUID, items []MemberInput) (int64, int64, error) {
		return 2, 1, nil
	}, venueFn: func(context.Context, string) (uuid.UUID, error) { return uuid.New(), nil }})
	w := doReq(t, testRouter(h, true), "POST", "/v1/trader-groups/"+id.String()+"/members",
		`{"members":[{"venue":"hyperliquid","wallet_address":"0xabcdef0123456789abcdef0123456789abcdef01"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["data"].(map[string]any)["added"] != float64(2) || resp["data"].(map[string]any)["skipped"] != float64(1) {
		t.Errorf("shape: %v", resp)
	}
	// store error 404
	h2 := NewHandler(&mockStore{addFn: func(context.Context, uuid.UUID, uuid.UUID, []MemberInput) (int64, int64, error) {
		return 0, 0, domain.NewError(domain.ErrCodeNotFound, "unknown wallet")
	}})
	w2 := doReq(t, testRouter(h2, true), "POST", "/v1/trader-groups/"+id.String()+"/members",
		`{"members":[{"venue":"hyperliquid","wallet_address":"0xabcdef0123456789abcdef0123456789abcdef01"}]}`)
	if w2.Code != http.StatusNotFound {
		t.Errorf("unknown wallet: got %d", w2.Code)
	}
	// foreign
	h3 := NewHandler(&mockStore{addFn: func(context.Context, uuid.UUID, uuid.UUID, []MemberInput) (int64, int64, error) {
		return 0, 0, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	w3 := doReq(t, testRouter(h3, true), "POST", "/v1/trader-groups/"+id.String()+"/members",
		`{"members":[{"venue":"hyperliquid","wallet_address":"0xabcdef0123456789abcdef0123456789abcdef01"}]}`)
	if w3.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w3.Code)
	}
	// anonymous
	h4 := NewHandler(&mockStore{})
	w4 := doReq(t, testRouter(h4, false), "POST", "/v1/trader-groups/"+id.String()+"/members", `{"members":[]}`)
	if w4.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w4.Code)
	}
	// bad uuid
	h5 := NewHandler(&mockStore{})
	w5 := doReq(t, testRouter(h5, true), "POST", "/v1/trader-groups/nope/members", `{"members":[]}`)
	if w5.Code != http.StatusBadRequest {
		t.Errorf("bad uuid: got %d", w5.Code)
	}
}

func TestHandler_RemoveMembers_Extra(t *testing.T) {
	id := uuid.New()
	h := NewHandler(&mockStore{removeFn: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, []string) (int64, error) {
		return 1, nil
	}, venueFn: func(context.Context, string) (uuid.UUID, error) { return uuid.New(), nil }})
	w := doReq(t, testRouter(h, true), "DELETE", "/v1/trader-groups/"+id.String()+"/members",
		`{"members":[{"venue":"hyperliquid","wallet_address":"0xabcdef0123456789abcdef0123456789abcdef01"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	// store error
	h2 := NewHandler(&mockStore{removeFn: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, []string) (int64, error) {
		return 0, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}, venueFn: func(context.Context, string) (uuid.UUID, error) { return uuid.New(), nil }})
	w2 := doReq(t, testRouter(h2, true), "DELETE", "/v1/trader-groups/"+id.String()+"/members",
		`{"members":[{"venue":"hyperliquid","wallet_address":"0xabcdef0123456789abcdef0123456789abcdef01"}]}`)
	if w2.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w2.Code)
	}
	// anonymous
	h3 := NewHandler(&mockStore{})
	w3 := doReq(t, testRouter(h3, false), "DELETE", "/v1/trader-groups/"+id.String()+"/members", `{"members":[]}`)
	if w3.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w3.Code)
	}
	// bad uuid
	h4 := NewHandler(&mockStore{})
	w4 := doReq(t, testRouter(h4, true), "DELETE", "/v1/trader-groups/nope/members", `{"members":[]}`)
	if w4.Code != http.StatusBadRequest {
		t.Errorf("bad uuid: got %d", w4.Code)
	}
}

func TestHandler_ListMembers_Extra(t *testing.T) {
	id := uuid.New()
	h := NewHandler(&mockStore{listMembersFn: func(_ context.Context, g, u uuid.UUID, params ListMembersParams) ([]*MemberRow, int, error) {
		return []*MemberRow{{VenueID: uuid.New(), Venue: "hyperliquid", WalletAddress: "0xabc", DataStatus: "ready"}}, 1, nil
	}})
	w := doReq(t, testRouter(h, true), "GET", "/v1/trader-groups/"+id.String()+"/members?page=1&page_size=20", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	// pagination meta
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["meta"] == nil {
		t.Errorf("missing meta")
	}
	// invalid page
	w2 := doReq(t, testRouter(h, true), "GET", "/v1/trader-groups/"+id.String()+"/members?page=0", "")
	if w2.Code != http.StatusBadRequest || errCode(t, w2) != "INVALID_FILTER" {
		t.Errorf("invalid page: got %d %s", w2.Code, w2.Body.String())
	}
	// invalid period
	h2 := NewHandler(&mockStore{listMembersFn: func(context.Context, uuid.UUID, uuid.UUID, ListMembersParams) ([]*MemberRow, int, error) {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "bad")
	}})
	w3 := doReq(t, testRouter(h2, true), "GET", "/v1/trader-groups/"+id.String()+"/members?period=BAD", "")
	if w3.Code != http.StatusBadRequest {
		t.Errorf("bad period: got %d", w3.Code)
	}
	// foreign
	h3 := NewHandler(&mockStore{listMembersFn: func(context.Context, uuid.UUID, uuid.UUID, ListMembersParams) ([]*MemberRow, int, error) {
		return nil, 0, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	w4 := doReq(t, testRouter(h3, true), "GET", "/v1/trader-groups/"+id.String()+"/members", "")
	if w4.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w4.Code)
	}
	// anonymous
	h4 := NewHandler(&mockStore{})
	w5 := doReq(t, testRouter(h4, false), "GET", "/v1/trader-groups/"+id.String()+"/members", "")
	if w5.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w5.Code)
	}
	// bad uuid
	h5 := NewHandler(&mockStore{})
	w6 := doReq(t, testRouter(h5, true), "GET", "/v1/trader-groups/nope/members", "")
	if w6.Code != http.StatusBadRequest {
		t.Errorf("bad uuid: got %d", w6.Code)
	}
}

func TestHandler_Overview_Extra(t *testing.T) {
	h := NewHandler(&mockStore{overviewFn: func(_ context.Context, u uuid.UUID, period string) (*OverviewStats, error) {
		return &OverviewStats{TotalGroups: 1, AsOf: time.Now().UTC()}, nil
	}})
	w := doReq(t, testRouter(h, true), "GET", "/v1/trader-groups/overview?period=30D", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	// error
	h2 := NewHandler(&mockStore{overviewFn: func(context.Context, uuid.UUID, string) (*OverviewStats, error) {
		return nil, errors.New("db gone")
	}})
	w2 := doReq(t, testRouter(h2, true), "GET", "/v1/trader-groups/overview", "")
	if w2.Code != http.StatusInternalServerError {
		t.Errorf("raw err: got %d", w2.Code)
	}
	// anonymous
	h3 := NewHandler(&mockStore{})
	w3 := doReq(t, testRouter(h3, false), "GET", "/v1/trader-groups/overview", "")
	if w3.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w3.Code)
	}
}

func TestHandler_Favorite_Extra(t *testing.T) {
	id := uuid.New()
	// 404/403
	h := NewHandler(&mockStore{toggleFavoriteFn: func(context.Context, uuid.UUID, uuid.UUID, bool) (*Group, error) {
		return nil, domain.NewError(domain.ErrCodeNotFound, "nope")
	}})
	w := doReq(t, testRouter(h, true), "PATCH", "/v1/trader-groups/"+id.String()+"/favorite", `{"is_favorite":true}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown: got %d", w.Code)
	}
	h2 := NewHandler(&mockStore{toggleFavoriteFn: func(context.Context, uuid.UUID, uuid.UUID, bool) (*Group, error) {
		return nil, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	w2 := doReq(t, testRouter(h2, true), "PATCH", "/v1/trader-groups/"+id.String()+"/favorite", `{"is_favorite":true}`)
	if w2.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w2.Code)
	}
	// anonymous
	h3 := NewHandler(&mockStore{})
	w3 := doReq(t, testRouter(h3, false), "PATCH", "/v1/trader-groups/"+id.String()+"/favorite", `{"is_favorite":true}`)
	if w3.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w3.Code)
	}
	// bad body
	h4 := NewHandler(&mockStore{})
	w4 := doReq(t, testRouter(h4, true), "PATCH", "/v1/trader-groups/"+id.String()+"/favorite", `{`)
	if w4.Code != http.StatusBadRequest {
		t.Errorf("bad body: got %d", w4.Code)
	}
	// bad uuid
	h5 := NewHandler(&mockStore{})
	w5 := doReq(t, testRouter(h5, true), "PATCH", "/v1/trader-groups/nope/favorite", `{"is_favorite":true}`)
	if w5.Code != http.StatusBadRequest {
		t.Errorf("bad uuid: got %d", w5.Code)
	}
}

func TestHandler_Duplicate_Extra(t *testing.T) {
	id := uuid.New()
	// happy with name
	h := NewHandler(&mockStore{duplicateFn: func(_ context.Context, g, u uuid.UUID, name *string) (*Group, error) {
		return &Group{ID: uuid.New(), Name: "Copy"}, nil
	}})
	w := doReq(t, testRouter(h, true), "POST", "/v1/trader-groups/"+id.String()+"/duplicate", `{"name":"Valid"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	// invalid name
	h2 := NewHandler(&mockStore{duplicateFn: func(context.Context, uuid.UUID, uuid.UUID, *string) (*Group, error) {
		return nil, nil
	}})
	long := strings.Repeat("x", 101)
	w2 := doReq(t, testRouter(h2, true), "POST", "/v1/trader-groups/"+id.String()+"/duplicate", `{"name":"`+long+`"}`)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("long name: got %d", w2.Code)
	}
	// 404/403
	h3 := NewHandler(&mockStore{duplicateFn: func(context.Context, uuid.UUID, uuid.UUID, *string) (*Group, error) {
		return nil, domain.NewError(domain.ErrCodeNotFound, "nope")
	}})
	w3 := doReq(t, testRouter(h3, true), "POST", "/v1/trader-groups/"+id.String()+"/duplicate", `{}`)
	if w3.Code != http.StatusNotFound {
		t.Errorf("unknown: got %d", w3.Code)
	}
	h4 := NewHandler(&mockStore{duplicateFn: func(context.Context, uuid.UUID, uuid.UUID, *string) (*Group, error) {
		return nil, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	w4 := doReq(t, testRouter(h4, true), "POST", "/v1/trader-groups/"+id.String()+"/duplicate", `{}`)
	if w4.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w4.Code)
	}
	// anonymous
	h5 := NewHandler(&mockStore{})
	w5 := doReq(t, testRouter(h5, false), "POST", "/v1/trader-groups/"+id.String()+"/duplicate", `{}`)
	if w5.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w5.Code)
	}
	// bad uuid
	h6 := NewHandler(&mockStore{})
	w6 := doReq(t, testRouter(h6, true), "POST", "/v1/trader-groups/nope/duplicate", `{}`)
	if w6.Code != http.StatusBadRequest {
		t.Errorf("bad uuid: got %d", w6.Code)
	}
}

func TestHandler_List_Extra_Validation(t *testing.T) {
	h := NewHandler(&mockStore{listFn: func(context.Context, uuid.UUID, ListGroupsParams) ([]*GroupListItem, int, error) {
		return []*GroupListItem{}, 0, nil
	}})
	// invalid status
	w := doReq(t, testRouter(h, true), "GET", "/v1/trader-groups?status=foo", "")
	if w.Code != http.StatusBadRequest || errCode(t, w) != "INVALID_FILTER" {
		t.Errorf("bad status: got %d %s", w.Code, w.Body.String())
	}
	// invalid page_size
	w2 := doReq(t, testRouter(h, true), "GET", "/v1/trader-groups?page_size=0", "")
	if w2.Code != http.StatusBadRequest || errCode(t, w2) != "INVALID_FILTER" {
		t.Errorf("bad pagesize: got %d %s", w2.Code, w2.Body.String())
	}
	// invalid favorite
	w3 := doReq(t, testRouter(h, true), "GET", "/v1/trader-groups?favorite=maybe", "")
	if w3.Code != http.StatusBadRequest || errCode(t, w3) != "INVALID_FILTER" {
		t.Errorf("bad favorite: got %d %s", w3.Code, w3.Body.String())
	}
	// anonymous
	h2 := NewHandler(&mockStore{})
	w4 := doReq(t, testRouter(h2, false), "GET", "/v1/trader-groups", "")
	if w4.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w4.Code)
	}
}

func TestHandler_ListMembers_FilterValidation_Extra(t *testing.T) {
	id := uuid.New()
	h := NewHandler(&mockStore{listMembersFn: func(context.Context, uuid.UUID, uuid.UUID, ListMembersParams) ([]*MemberRow, int, error) {
		return []*MemberRow{}, 0, nil
	}})
	w := doReq(t, testRouter(h, true), "GET", "/v1/trader-groups/"+id.String()+"/members?filter=foo", "")
	if w.Code != http.StatusBadRequest || errCode(t, w) != "INVALID_FILTER" {
		t.Errorf("bad filter: got %d %s", w.Code, w.Body.String())
	}
}
