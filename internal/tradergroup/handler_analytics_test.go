package tradergroup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// STATS-01: happy path, explicit period forwarded, invalid period rejected.
func TestHandler_Stats(t *testing.T) {
	id := uuid.New()
	var gotPeriod string
	h := NewHandler(&mockStore{statsFn: func(_ context.Context, g, u uuid.UUID, period string) (*GroupStats, error) {
		if g != id || u.String() != authedUserID {
			t.Errorf("args: %v %v", g, u)
		}
		gotPeriod = period
		return &GroupStats{Period: period, MembersTotal: 2, MembersWithData: 2}, nil
	}})
	w := doReq(t, testRouter(h, true), "GET", "/v1/trader-groups/"+id.String()+"/stats", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	if gotPeriod != "30D" {
		t.Errorf("default period: got %q", gotPeriod)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["data"].(map[string]any)["members_total"] != float64(2) {
		t.Errorf("shape: %v", resp)
	}

	w2 := doReq(t, testRouter(h, true), "GET", "/v1/trader-groups/"+id.String()+"/stats?period=90D", "")
	if w2.Code != http.StatusOK {
		t.Fatalf("90D: got %d %s", w2.Code, w2.Body.String())
	}
	if gotPeriod != "90D" {
		t.Errorf("explicit period: got %q", gotPeriod)
	}

	// Invalid period must not reach the store.
	called := false
	h2 := NewHandler(&mockStore{statsFn: func(context.Context, uuid.UUID, uuid.UUID, string) (*GroupStats, error) {
		called = true
		return &GroupStats{}, nil
	}})
	w3 := doReq(t, testRouter(h2, true), "GET", "/v1/trader-groups/"+id.String()+"/stats?period=BAD", "")
	if w3.Code != http.StatusBadRequest || errCode(t, w3) != "INVALID_PERIOD" || called {
		t.Errorf("bad period: got %d %s called=%v", w3.Code, w3.Body.String(), called)
	}
}

// STATS-02: auth, unknown, foreign and malformed id.
func TestHandler_Stats_Errors(t *testing.T) {
	id := uuid.New()
	if w := doReq(t, testRouter(NewHandler(&mockStore{}), false), "GET", "/v1/trader-groups/"+id.String()+"/stats", ""); w.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w.Code)
	}
	if w := doReq(t, testRouter(NewHandler(&mockStore{}), true), "GET", "/v1/trader-groups/nope/stats", ""); w.Code != http.StatusBadRequest {
		t.Errorf("bad uuid: got %d", w.Code)
	}
	h404 := NewHandler(&mockStore{statsFn: func(context.Context, uuid.UUID, uuid.UUID, string) (*GroupStats, error) {
		return nil, domain.NewError(domain.ErrCodeNotFound, "nope")
	}})
	if w := doReq(t, testRouter(h404, true), "GET", "/v1/trader-groups/"+id.String()+"/stats", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown: got %d", w.Code)
	}
	h403 := NewHandler(&mockStore{statsFn: func(context.Context, uuid.UUID, uuid.UUID, string) (*GroupStats, error) {
		return nil, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	if w := doReq(t, testRouter(h403, true), "GET", "/v1/trader-groups/"+id.String()+"/stats", ""); w.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w.Code)
	}
}

// POS-01: happy path returns the aggregated payload.
func TestHandler_Positions(t *testing.T) {
	id := uuid.New()
	h := NewHandler(&mockStore{positionsFn: func(_ context.Context, g, u uuid.UUID) (*PositionsResponse, error) {
		if g != id || u.String() != authedUserID {
			t.Errorf("args: %v %v", g, u)
		}
		return &PositionsResponse{
			DataStatus: "ready",
			Totals:     PositionTotals{LongNtl: 500, ShortNtl: 300, NetNtl: 200, GrossNtl: 800, OpenPositions: 2},
			Positions:  []PositionRow{},
		}, nil
	}})
	w := doReq(t, testRouter(h, true), "GET", "/v1/trader-groups/"+id.String()+"/positions", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]any)
	if data["data_status"] != "ready" {
		t.Errorf("shape: %v", data)
	}
	if data["totals"].(map[string]any)["gross_ntl"] != float64(800) {
		t.Errorf("totals: %v", data["totals"])
	}
}

// POS-02: auth, unknown, foreign and malformed id.
func TestHandler_Positions_Errors(t *testing.T) {
	id := uuid.New()
	if w := doReq(t, testRouter(NewHandler(&mockStore{}), false), "GET", "/v1/trader-groups/"+id.String()+"/positions", ""); w.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w.Code)
	}
	if w := doReq(t, testRouter(NewHandler(&mockStore{}), true), "GET", "/v1/trader-groups/nope/positions", ""); w.Code != http.StatusBadRequest {
		t.Errorf("bad uuid: got %d", w.Code)
	}
	h404 := NewHandler(&mockStore{positionsFn: func(context.Context, uuid.UUID, uuid.UUID) (*PositionsResponse, error) {
		return nil, domain.NewError(domain.ErrCodeNotFound, "nope")
	}})
	if w := doReq(t, testRouter(h404, true), "GET", "/v1/trader-groups/"+id.String()+"/positions", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown: got %d", w.Code)
	}
	h403 := NewHandler(&mockStore{positionsFn: func(context.Context, uuid.UUID, uuid.UUID) (*PositionsResponse, error) {
		return nil, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	if w := doReq(t, testRouter(h403, true), "GET", "/v1/trader-groups/"+id.String()+"/positions", ""); w.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w.Code)
	}
}

// PERF-01: happy path, period forwarded, invalid period rejected.
func TestHandler_Performance(t *testing.T) {
	id := uuid.New()
	var gotPeriod string
	h := NewHandler(&mockStore{performanceFn: func(_ context.Context, g, u uuid.UUID, period string) (*PerformanceResponse, error) {
		if g != id || u.String() != authedUserID {
			t.Errorf("args: %v %v", g, u)
		}
		gotPeriod = period
		return &PerformanceResponse{Period: period, Granularity: "daily"}, nil
	}})
	w := doReq(t, testRouter(h, true), "GET", "/v1/trader-groups/"+id.String()+"/performance?period=7D", "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	if gotPeriod != "7D" {
		t.Errorf("period: got %q", gotPeriod)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["data"].(map[string]any)["granularity"] != "daily" {
		t.Errorf("shape: %v", resp)
	}

	called := false
	h2 := NewHandler(&mockStore{performanceFn: func(context.Context, uuid.UUID, uuid.UUID, string) (*PerformanceResponse, error) {
		called = true
		return &PerformanceResponse{}, nil
	}})
	w2 := doReq(t, testRouter(h2, true), "GET", "/v1/trader-groups/"+id.String()+"/performance?period=1Y", "")
	if w2.Code != http.StatusBadRequest || errCode(t, w2) != "INVALID_PERIOD" || called {
		t.Errorf("bad period: got %d %s called=%v", w2.Code, w2.Body.String(), called)
	}
}

// PERF-02: auth, unknown, foreign and malformed id.
func TestHandler_Performance_Errors(t *testing.T) {
	id := uuid.New()
	if w := doReq(t, testRouter(NewHandler(&mockStore{}), false), "GET", "/v1/trader-groups/"+id.String()+"/performance", ""); w.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w.Code)
	}
	if w := doReq(t, testRouter(NewHandler(&mockStore{}), true), "GET", "/v1/trader-groups/nope/performance", ""); w.Code != http.StatusBadRequest {
		t.Errorf("bad uuid: got %d", w.Code)
	}
	h404 := NewHandler(&mockStore{performanceFn: func(context.Context, uuid.UUID, uuid.UUID, string) (*PerformanceResponse, error) {
		return nil, domain.NewError(domain.ErrCodeNotFound, "nope")
	}})
	if w := doReq(t, testRouter(h404, true), "GET", "/v1/trader-groups/"+id.String()+"/performance", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown: got %d", w.Code)
	}
	h403 := NewHandler(&mockStore{performanceFn: func(context.Context, uuid.UUID, uuid.UUID, string) (*PerformanceResponse, error) {
		return nil, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	if w := doReq(t, testRouter(h403, true), "GET", "/v1/trader-groups/"+id.String()+"/performance", ""); w.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w.Code)
	}
}

// CMP-01: happy path forwards ids and period.
func TestHandler_Compare(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	h := NewHandler(&mockStore{compareFn: func(_ context.Context, u uuid.UUID, gids []uuid.UUID, period string) (*CompareResponse, error) {
		if u.String() != authedUserID {
			t.Errorf("user: %v", u)
		}
		if len(gids) != 2 || gids[0] != a || gids[1] != b {
			t.Errorf("ids: %v", gids)
		}
		if period != "90D" {
			t.Errorf("period: %q", period)
		}
		return &CompareResponse{Period: period, Groups: []CompareGroup{}, Series: []CompareSeries{}}, nil
	}})
	body := fmt.Sprintf(`{"group_ids":["%s","%s"],"period":"90D"}`, a, b)
	w := doReq(t, testRouter(h, true), "POST", "/v1/trader-groups/compare", body)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["data"].(map[string]any)["period"] != "90D" {
		t.Errorf("shape: %v", resp)
	}

	// Default period.
	var gotPeriod string
	h2 := NewHandler(&mockStore{compareFn: func(_ context.Context, _ uuid.UUID, _ []uuid.UUID, period string) (*CompareResponse, error) {
		gotPeriod = period
		return &CompareResponse{}, nil
	}})
	if w := doReq(t, testRouter(h2, true), "POST", "/v1/trader-groups/compare", fmt.Sprintf(`{"group_ids":["%s","%s"]}`, a, b)); w.Code != http.StatusOK {
		t.Fatalf("default: got %d %s", w.Code, w.Body.String())
	}
	if gotPeriod != "30D" {
		t.Errorf("default period: got %q", gotPeriod)
	}
}

// CMP-02: cardinality and period validation.
func TestHandler_Compare_Validation(t *testing.T) {
	id := uuid.NewString()
	called := false
	h := NewHandler(&mockStore{compareFn: func(context.Context, uuid.UUID, []uuid.UUID, string) (*CompareResponse, error) {
		called = true
		return &CompareResponse{}, nil
	}})

	// zero groups
	w := doReq(t, testRouter(h, true), "POST", "/v1/trader-groups/compare", `{"group_ids":[]}`)
	if w.Code != http.StatusBadRequest || errCode(t, w) != "COMPARE_TOO_FEW" {
		t.Errorf("zero: got %d %s", w.Code, w.Body.String())
	}
	// one group
	w = doReq(t, testRouter(h, true), "POST", "/v1/trader-groups/compare", fmt.Sprintf(`{"group_ids":["%s"]}`, id))
	if w.Code != http.StatusBadRequest || errCode(t, w) != "COMPARE_TOO_FEW" {
		t.Errorf("one: got %d %s", w.Code, w.Body.String())
	}
	// eleven groups
	ids := make([]string, 0, 11)
	for i := 0; i < 11; i++ {
		ids = append(ids, `"`+uuid.NewString()+`"`)
	}
	body := fmt.Sprintf(`{"group_ids":[%s]}`, strings.Join(ids, ","))
	w = doReq(t, testRouter(h, true), "POST", "/v1/trader-groups/compare", body)
	if w.Code != http.StatusBadRequest || errCode(t, w) != "COMPARE_TOO_MANY" {
		t.Errorf("eleven: got %d %s", w.Code, w.Body.String())
	}
	// invalid period
	w = doReq(t, testRouter(h, true), "POST", "/v1/trader-groups/compare",
		fmt.Sprintf(`{"group_ids":["%s","%s"],"period":"2D"}`, id, uuid.NewString()))
	if w.Code != http.StatusBadRequest || errCode(t, w) != "INVALID_PERIOD" {
		t.Errorf("period: got %d %s", w.Code, w.Body.String())
	}
	// malformed id
	w = doReq(t, testRouter(h, true), "POST", "/v1/trader-groups/compare", `{"group_ids":["nope","`+uuid.NewString()+`"]}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad id: got %d %s", w.Code, w.Body.String())
	}
	// malformed body
	w = doReq(t, testRouter(h, true), "POST", "/v1/trader-groups/compare", `{`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad body: got %d %s", w.Code, w.Body.String())
	}
	if called {
		t.Error("store must not be called on validation failure")
	}
}

// CMP-03: auth, unknown and foreign groups.
func TestHandler_Compare_Errors(t *testing.T) {
	a, b := uuid.NewString(), uuid.NewString()
	body := fmt.Sprintf(`{"group_ids":["%s","%s"]}`, a, b)

	if w := doReq(t, testRouter(NewHandler(&mockStore{}), false), "POST", "/v1/trader-groups/compare", body); w.Code != http.StatusForbidden {
		t.Errorf("anon: got %d", w.Code)
	}
	h404 := NewHandler(&mockStore{compareFn: func(context.Context, uuid.UUID, []uuid.UUID, string) (*CompareResponse, error) {
		return nil, domain.NewError(domain.ErrCodeNotFound, "nope")
	}})
	if w := doReq(t, testRouter(h404, true), "POST", "/v1/trader-groups/compare", body); w.Code != http.StatusNotFound {
		t.Errorf("unknown: got %d", w.Code)
	}
	h403 := NewHandler(&mockStore{compareFn: func(context.Context, uuid.UUID, []uuid.UUID, string) (*CompareResponse, error) {
		return nil, domain.NewError(domain.ErrCodeGroupForbidden, "nope")
	}})
	if w := doReq(t, testRouter(h403, true), "POST", "/v1/trader-groups/compare", body); w.Code != http.StatusForbidden {
		t.Errorf("foreign: got %d", w.Code)
	}
}
