package trader

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

func parseFloatPtr(s string) *float64 {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return nil
	}
	return &f
}

func parseFloat(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// Balances returns perp + spot summaries on-demand (LIVE-CONTRACT v1.2 §1.3).
// Source stays clearinghouseState + spotClearinghouseState behind the
// interactive client (own pacer, concurrency gate, ctx timeout) + TTL 12s
// cache + single-flight. Either side may be null on partial failure;
// data_status ready only when both succeed, error otherwise (stale cache
// returned when available). Keyed by (balances,wallet,venue).
func (s *Service) Balances(ctx context.Context, venueCode, rawAddr string) (*BalancesDTO, error) {
	addr, err := NormalizeAddress(rawAddr)
	if err != nil {
		return nil, err
	}
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	if _, err := s.repo.GetRegistry(ctx, venueID, addr); err != nil {
		return nil, err
	}
	key := "balances|" + venue + "|" + addr
	val, _, _, fetchErr := s.cache.GetOrFetch(key, func() (any, error) {
		if s.onDemand == nil {
			return nil, errUpstreamUnavailable
		}
		now := time.Now().UTC()
		var perp *PerpBalancesDTO
		var spot *SpotBalancesDTO
		var perpErr, spotErr error
		// Perp via clearinghouseState (incl. withdrawable + crossMarginSummary).
		func() {
			state, err := s.onDemand.FetchClearinghouseState(ctx, addr)
			if err != nil {
				perpErr = err
				return
			}
			p := &PerpBalancesDTO{AsOf: &now}
			p.AccountValue = optFloat(state.MarginSummary.AccountValue)
			p.TotalNtlPos = optFloat(state.MarginSummary.TotalNtlPos)
			p.TotalMarginUsed = optFloat(state.MarginSummary.TotalMarginUsed)
			p.Withdrawable = optFloat(state.Withdrawable)
			p.CrossAccountValue = optFloat(state.CrossMarginSummary.AccountValue)
			p.CrossTotalNtlPos = optFloat(state.CrossMarginSummary.TotalNtlPos)
			p.CrossTotalMarginUsed = optFloat(state.CrossMarginSummary.TotalMarginUsed)
			var sum float64
			for _, ap := range state.AssetPositions {
				if v, err := ap.Position.PositionValue.Float(); err == nil {
					if v < 0 {
						v = -v
					}
					sum += v
				}
			}
			sumCopy := sum
			p.AssetPositionsValue = &sumCopy
			perp = p
		}()
		func() {
			st, err := s.onDemand.FetchSpotState(ctx, addr)
			if err != nil {
				spotErr = err
				return
			}
			rows := make([]SpotBalanceDTO, 0, len(st.Balances))
			for _, b := range st.Balances {
				rows = append(rows, SpotBalanceDTO{
					Coin: strings.ToUpper(strings.TrimSpace(b.Coin)), Token: b.Token,
					Total: parseFloatPtr(b.Total), Hold: parseFloatPtr(b.Hold),
					EntryNtl: parseFloatPtr(b.EntryNtl),
				})
			}
			t := now
			spot = &SpotBalancesDTO{Balances: rows, AsOf: &t}
		}()
		if perp == nil && spot == nil {
			// Both sides failed: do not cache empty; caller degrades to
			// stale or empty with data_status=error.
			if perpErr != nil {
				return nil, perpErr
			}
			return nil, spotErr
		}
		status := DataReady
		if perpErr != nil || spotErr != nil {
			status = DataError
		}
		return &BalancesDTO{Perp: perp, Spot: spot, DataStatus: status}, nil
	})
	if fetchErr != nil {
		if stale, _, found := s.cache.get(key); found {
			if dto, ok := stale.(*BalancesDTO); ok {
				cpy := *dto
				cpy.DataStatus = DataError
				return &cpy, nil
			}
		}
		return &BalancesDTO{Perp: nil, Spot: nil, DataStatus: DataError}, nil
	}
	if dto, ok := val.(*BalancesDTO); ok {
		return dto, nil
	}
	return &BalancesDTO{Perp: nil, Spot: nil, DataStatus: DataError}, nil
}

func optFloat(d hyperliquid.DecimalString) *float64 {
	t := strings.TrimSpace(string(d))
	if t == "" {
		return nil
	}
	f, err := d.Float()
	if err != nil {
		return nil
	}
	return &f
}

// Fills returns user fills newest-first with tid keyset pagination
// (LIVE-CONTRACT v1.2 §1.4, unchanged shape, interactive client). The fetched
// 2000-row set is cached 12s keyed (fills,wallet,venue) + single-flight; FE
// paginates through it. On HL error the last cached set is paged, else empty
// rows (never null).
func (s *Service) Fills(ctx context.Context, venueCode, rawAddr string, limit int, cursor string) (*FillsPage, error) {
	addr, err := NormalizeAddress(rawAddr)
	if err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 200 {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..200")
	}
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	if _, err := s.repo.GetRegistry(ctx, venueID, addr); err != nil {
		return nil, err
	}
	key := "fills|" + venue + "|" + addr
	val, _, _, fetchErr := s.cache.GetOrFetch(key, func() (any, error) {
		if s.onDemand == nil {
			return nil, errUpstreamUnavailable
		}
		raw, err := s.onDemand.FetchUserFills(ctx, addr)
		if err != nil {
			return nil, err
		}
		return mapUserFills(raw), nil
	})
	var all []FillDTO
	if fetchErr != nil {
		if stale, _, found := s.cache.get(key); found {
			if v, ok := stale.([]FillDTO); ok {
				all = v
			}
		}
		if all == nil {
			return &FillsPage{Rows: []FillDTO{}}, nil
		}
	} else {
		if v, ok := val.([]FillDTO); ok {
			all = v
		} else {
			all = []FillDTO{}
		}
	}
	fp := fillsFingerprint(venueID, addr)
	var afterTime *time.Time
	var afterTid int64
	hasCursor := false
	if cursor != "" {
		t, tid, err := decodeFillsCursor(s.secret, fp, cursor)
		if err != nil {
			return nil, err
		}
		afterTime, afterTid, hasCursor = &t, tid, true
	}
	paged := make([]FillDTO, 0, len(all))
	for _, r := range all {
		if hasCursor {
			if r.Time.After(*afterTime) {
				continue
			}
			if r.Time.Equal(*afterTime) && r.Tid >= afterTid {
				continue
			}
		}
		paged = append(paged, r)
	}
	hasMore := len(paged) > limit
	if hasMore {
		paged = paged[:limit]
	}
	page := &FillsPage{Rows: paged, HasMore: hasMore}
	if page.Rows == nil {
		page.Rows = []FillDTO{}
	}
	if hasMore {
		last := paged[len(paged)-1]
		cur, err := encodeFillsCursor(s.secret, fp, last.Time, last.Tid)
		if err != nil {
			return nil, err
		}
		page.NextCursor = cur
	}
	return page, nil
}

func mapUserFills(raw []hyperliquid.Fill) []FillDTO {
	out := make([]FillDTO, 0, len(raw))
	for _, f := range raw {
		side := normalizeSideForOrder(f.Side)
		if side != "BUY" && side != "SELL" {
			continue
		}
		sz, ok := parseFloat(f.Sz)
		if !ok || sz <= 0 {
			continue
		}
		px, ok := parseFloat(f.Px)
		if !ok || px < 0 {
			continue
		}
		if f.Time <= 0 {
			continue
		}
		closed, _ := parseFloat(f.ClosedPnl)
		fee, _ := parseFloat(f.Fee)
		out = append(out, FillDTO{
			Coin: strings.ToUpper(strings.TrimSpace(f.Coin)), Side: side, Dir: f.Dir,
			Size: sz, Price: px, ClosedPnl: closed, Fee: fee, FeeToken: f.FeeToken,
			Time: time.UnixMilli(f.Time).UTC(), Tid: f.Tid, Oid: f.Oid,
			Crossed: f.Crossed, StartPosition: f.StartPosition,
		})
	}
	// Newest-first (time DESC, tid DESC).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j], out[j-1]
			swap := a.Time.After(b.Time) || (a.Time.Equal(b.Time) && a.Tid > b.Tid)
			// out is built oldest-unknown; insertion keeps newest-first:
			// swap when a is newer than b.
			if !swap {
				break
			}
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	// The loop above sorts ascending-newest? Re-verify: we want newest first.
	// Insertion as written bubbles newer toward front — correct.
	return out
}

// Orders returns open or historical orders (LIVE-CONTRACT v1.2 §1.5,
// unchanged shape, interactive client). Cached 12s keyed
// (orders,wallet,venue,status) + single-flight; limit slices the cached set.
// order_status / status_timestamp present only for historical.
func (s *Service) Orders(ctx context.Context, venueCode, rawAddr, status string, limit int) (*OrdersDTO, error) {
	addr, err := NormalizeAddress(rawAddr)
	if err != nil {
		return nil, err
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		status = "open"
	}
	if status != "open" && status != "historical" {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid status: want open|historical")
	}
	if limit == 0 {
		limit = 200
	}
	if limit < 1 || limit > 2000 {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..2000")
	}
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	if _, err := s.repo.GetRegistry(ctx, venueID, addr); err != nil {
		return nil, err
	}
	key := "orders|" + venue + "|" + addr + "|" + status
	val, _, _, fetchErr := s.cache.GetOrFetch(key, func() (any, error) {
		if s.onDemand == nil {
			return nil, errUpstreamUnavailable
		}
		if status == "open" {
			raw, err := s.onDemand.FetchOpenOrders(ctx, addr)
			if err != nil {
				return nil, err
			}
			return mapOpenOrders(raw, false), nil
		}
		raw, err := s.onDemand.FetchHistoricalOrders(ctx, addr)
		if err != nil {
			return nil, err
		}
		return mapHistoricalOrders(raw), nil
	})
	var all []OrderDTO
	if fetchErr != nil {
		if stale, _, found := s.cache.get(key); found {
			if v, ok := stale.([]OrderDTO); ok {
				all = v
			}
		}
		if all == nil {
			all = []OrderDTO{}
		}
	} else {
		if v, ok := val.([]OrderDTO); ok {
			all = v
		} else {
			all = []OrderDTO{}
		}
	}
	if len(all) > limit {
		all = all[:limit]
	}
	if all == nil {
		all = []OrderDTO{}
	}
	return &OrdersDTO{Status: status, Rows: all}, nil
}

func mapOpenOrders(raw []hyperliquid.OpenOrder, _ bool) []OrderDTO {
	out := make([]OrderDTO, 0, len(raw))
	for _, o := range raw {
		side := normalizeSideForOrder(o.Side)
		if side != "BUY" && side != "SELL" {
			continue
		}
		coin := strings.ToUpper(strings.TrimSpace(o.Coin))
		if coin == "" {
			continue
		}
		limitPx, ok := parseFloat(o.LimitPx)
		if !ok {
			continue
		}
		sz, ok := parseFloat(o.Sz)
		if !ok || sz <= 0 {
			continue
		}
		origSz, ok := parseFloat(o.OrigSz)
		if !ok || origSz <= 0 {
			origSz = sz
		}
		if o.Timestamp <= 0 {
			continue
		}
		dto := OrderDTO{
			Coin: coin, Side: side, LimitPx: limitPx, Size: sz, OrigSize: origSz,
			Oid: o.Oid, Timestamp: time.UnixMilli(o.Timestamp).UTC(),
			ReduceOnly: o.ReduceOnly, OrderType: o.OrderType,
			TriggerCondition: o.TriggerCondition,
			TriggerPx:        parseFloatPtr(o.TriggerPx),
		}
		if o.IsPositionTpsl != nil {
			dto.IsPositionTpsl = *o.IsPositionTpsl
		}
		out = append(out, dto)
	}
	return out
}

func mapHistoricalOrders(raw []hyperliquid.HistoricalOrder) []OrderDTO {
	out := make([]OrderDTO, 0, len(raw))
	for _, h := range raw {
		o := h.Order
		side := normalizeSideForOrder(o.Side)
		if side != "BUY" && side != "SELL" {
			continue
		}
		coin := strings.ToUpper(strings.TrimSpace(o.Coin))
		if coin == "" {
			continue
		}
		limitPx, ok := parseFloat(o.LimitPx)
		if !ok {
			continue
		}
		sz, ok := parseFloat(o.Sz)
		if !ok || sz <= 0 {
			continue
		}
		origSz, ok := parseFloat(o.OrigSz)
		if !ok || origSz <= 0 {
			origSz = sz
		}
		if o.Timestamp <= 0 {
			continue
		}
		dto := OrderDTO{
			Coin: coin, Side: side, LimitPx: limitPx, Size: sz, OrigSize: origSz,
			Oid: o.Oid, Timestamp: time.UnixMilli(o.Timestamp).UTC(),
			ReduceOnly: o.ReduceOnly, OrderType: o.OrderType,
			TriggerCondition: o.TriggerCondition,
			TriggerPx:        parseFloatPtr(o.TriggerPx),
		}
		if o.IsPositionTpsl != nil {
			dto.IsPositionTpsl = *o.IsPositionTpsl
		}
		st := h.Status
		dto.OrderStatus = &st
		if h.StatusTimestamp > 0 {
			t := time.UnixMilli(h.StatusTimestamp).UTC()
			dto.StatusTimestamp = &t
		}
		out = append(out, dto)
	}
	return out
}

var transferTypeEnum = map[string]bool{
	"deposit": true, "withdraw": true, "internalTransfer": true,
	"subAccountTransfer": true, "accountClassTransfer": true,
	"spotTransfer": true, "send": true, "vaultDeposit": true,
	"vaultWithdraw": true, "vaultCreate": true, "vaultDistribution": true,
	"cStakingTransfer": true, "other": true,
}

// Transfers returns ledger transfers newest-first with time+hash keyset
// pagination (LIVE-CONTRACT v1.2 §1.6, unchanged shape, interactive client).
// Cached 12s keyed (transfers,wallet,venue,days) + single-flight. Unknown
// delta types map to "other"; funding never appears (non-funding source by
// decision).
func (s *Service) Transfers(ctx context.Context, venueCode, rawAddr string, days, limit int, cursor string) (*TransfersPage, error) {
	addr, err := NormalizeAddress(rawAddr)
	if err != nil {
		return nil, err
	}
	if days == 0 {
		days = 30
	}
	if days < 1 || days > 180 {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "days must be 1..180")
	}
	if limit == 0 {
		limit = 200
	}
	if limit < 1 || limit > 500 {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "limit must be 1..500")
	}
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	if _, err := s.repo.GetRegistry(ctx, venueID, addr); err != nil {
		return nil, err
	}
	key := "transfers|" + venue + "|" + addr + "|" + strconv.Itoa(days)
	val, _, _, fetchErr := s.cache.GetOrFetch(key, func() (any, error) {
		if s.onDemand == nil {
			return nil, errUpstreamUnavailable
		}
		startMs := time.Now().AddDate(0, 0, -days).UnixMilli()
		raw, err := s.onDemand.FetchLedgerUpdates(ctx, addr, startMs)
		if err != nil {
			return nil, err
		}
		return mapLedgerUpdates(raw), nil
	})
	var all []TransferDTO
	if fetchErr != nil {
		if stale, _, found := s.cache.get(key); found {
			if v, ok := stale.([]TransferDTO); ok {
				all = v
			}
		}
		if all == nil {
			all = []TransferDTO{}
		}
	} else {
		if v, ok := val.([]TransferDTO); ok {
			all = v
		} else {
			all = []TransferDTO{}
		}
	}
	fp := transfersFingerprint(venueID, addr, days)
	var afterTime *time.Time
	var afterHash string
	hasCursor := false
	if cursor != "" {
		t, h, err := decodeTransfersCursor(s.secret, fp, cursor)
		if err != nil {
			return nil, err
		}
		afterTime, afterHash, hasCursor = &t, h, true
	}
	paged := make([]TransferDTO, 0, len(all))
	for _, r := range all {
		if hasCursor {
			if r.Time.After(*afterTime) {
				continue
			}
			if r.Time.Equal(*afterTime) && r.Hash <= afterHash {
				continue
			}
		}
		paged = append(paged, r)
	}
	hasMore := len(paged) > limit
	if hasMore {
		paged = paged[:limit]
	}
	page := &TransfersPage{Rows: paged, HasMore: hasMore}
	if page.Rows == nil {
		page.Rows = []TransferDTO{}
	}
	if hasMore {
		last := paged[len(paged)-1]
		cur, err := encodeTransfersCursor(s.secret, fp, last.Time, last.Hash)
		if err != nil {
			return nil, err
		}
		page.NextCursor = cur
	}
	return page, nil
}

func mapLedgerUpdates(raw []hyperliquid.LedgerUpdate) []TransferDTO {
	out := make([]TransferDTO, 0, len(raw))
	for _, u := range raw {
		if u.Time <= 0 || strings.TrimSpace(u.Hash) == "" {
			continue
		}
		var delta map[string]json.RawMessage
		_ = json.Unmarshal(u.Delta, &delta)
		typeStr := ""
		if rawType, ok := delta["type"]; ok {
			_ = json.Unmarshal(rawType, &typeStr)
		}
		if !transferTypeEnum[typeStr] {
			typeStr = "other"
		}
		dto := TransferDTO{
			Time: time.UnixMilli(u.Time).UTC(), Hash: u.Hash, Type: typeStr,
		}
		dto.Token = strField(delta, "token")
		dto.Amount = floatField(delta, "amount", "sz", "size")
		dto.UsdcValue = floatField(delta, "usdcValue", "usdc_value", "value")
		dto.Usdc = floatField(delta, "usdc")
		dto.SourceDex = strField(delta, "sourceDex", "source_dex")
		dto.DestinationDex = strField(delta, "destinationDex", "destination_dex")
		dto.Counterparty = strField(delta, "counterparty", "user", "destination", "to", "from", "address")
		switch typeStr {
		case "deposit":
			t := true
			dto.IsDeposit = &t
		case "withdraw":
			f := false
			dto.IsDeposit = &f
		default:
			if b, ok := boolField(delta, "isDeposit", "is_deposit"); ok {
				dto.IsDeposit = b
			}
		}
		out = append(out, dto)
	}
	// Newest-first (time DESC, hash ASC).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j], out[j-1]
			swap := a.Time.After(b.Time) || (a.Time.Equal(b.Time) && a.Hash < b.Hash)
			if !swap {
				break
			}
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func strField(delta map[string]json.RawMessage, keys ...string) *string {
	for _, k := range keys {
		raw, ok := delta[k]
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) == nil && strings.TrimSpace(s) != "" {
			v := strings.TrimSpace(s)
			return &v
		}
	}
	return nil
}

func floatField(delta map[string]json.RawMessage, keys ...string) *float64 {
	for _, k := range keys {
		raw, ok := delta[k]
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if v := parseFloatPtr(s); v != nil {
				return v
			}
			continue
		}
		var f float64
		if json.Unmarshal(raw, &f) == nil {
			v := f
			return &v
		}
	}
	return nil
}

func boolField(delta map[string]json.RawMessage, keys ...string) (*bool, bool) {
	for _, k := range keys {
		raw, ok := delta[k]
		if !ok {
			continue
		}
		var b bool
		if json.Unmarshal(raw, &b) == nil {
			return &b, true
		}
	}
	return nil, false
}

// Performance returns LIVE overview metrics (LIVE-CONTRACT v1.2 §1.7, was DB).
// Metrics computed live from the same fills universe as §1.2 (1D/7D/30D from
// fills; ALL from portfolio/LB): roi (pnl/volume fallback), pnl (realized
// sum), win_rate, volume, trade_count, profit_factor, long/short wins+counts,
// max_drawdown (from live equity when available else null). Kỳ ALL: portfolio
// allTime + leaderboard refs passthrough (existing resolve semantics,
// live-read). Equity: live portfolio endpoint (reuse FetchPortfolio +
// AggregateEquityDaily). period ∈ {1D,7D,30D,ALL} default 30D. No reads of
// trader_period_metrics / trader_equity_daily (conformance §7); LB refs are
// read live. TTL 12s + single-flight per (wallet,venue,period); HL error ⇒
// stale with error else metrics null + equity [] (never null).
func (s *Service) Performance(ctx context.Context, venueCode, rawAddr, period string) (*PerformanceDTO, error) {
	addr, err := NormalizeAddress(rawAddr)
	if err != nil {
		return nil, err
	}
	period = strings.ToUpper(strings.TrimSpace(period))
	if period == "" {
		period = Period30D
	}
	switch period {
	case Period1D, Period7D, Period30D, PeriodALL:
	default:
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period")
	}
	venue := strings.ToLower(strings.TrimSpace(venueCode))
	if venue == "" {
		venue = DefaultVenue
	}
	venueID, err := s.repo.VenueIDByCode(ctx, venue)
	if err != nil {
		return nil, domain.NewError(domain.ErrCodeInvalidFilter, "unknown venue")
	}
	if _, err := s.repo.GetRegistry(ctx, venueID, addr); err != nil {
		return nil, err
	}
	key := "performance-live|" + venue + "|" + addr + "|" + period
	now := time.Now().UTC()
	val, _, _, fetchErr := s.cache.GetOrFetch(key, func() (any, error) {
		return fetchLivePerformance(ctx, s, venueID, addr, venue, period, now)
	})
	if fetchErr != nil {
		if stale, _, found := s.cache.get(key); found {
			if snap, ok := stale.(*LivePerformanceSnapshot); ok && snap != nil {
				cpy := *snap
				if cpy.Metrics != nil {
					mcpy := *cpy.Metrics
					mcpy.DataStatus = DataError
					cpy.Metrics = &mcpy
				} else {
					cpy.Metrics = &PerformanceMetricsDTO{DataStatus: DataError, IsPartial: true}
				}
				if cpy.Equity == nil {
					cpy.Equity = []EquityPointDTO{}
				}
				return &PerformanceDTO{Period: period, Metrics: cpy.Metrics, Equity: cpy.Equity}, nil
			}
		}
		return &PerformanceDTO{
			Period:  period,
			Metrics: &PerformanceMetricsDTO{DataStatus: DataError, IsPartial: true},
			Equity:  []EquityPointDTO{},
		}, nil
	}
	snap, ok := val.(*LivePerformanceSnapshot)
	if !ok || snap == nil {
		return &PerformanceDTO{
			Period:  period,
			Metrics: &PerformanceMetricsDTO{DataStatus: DataError, IsPartial: true},
			Equity:  []EquityPointDTO{},
		}, nil
	}
	equity := snap.Equity
	if equity == nil {
		equity = []EquityPointDTO{}
	}
	return &PerformanceDTO{Period: period, Metrics: snap.Metrics, Equity: equity}, nil
}

func periodLookback(period string) time.Duration {
	switch period {
	case Period1D:
		return 24 * time.Hour
	case Period7D:
		return 7 * 24 * time.Hour
	case PeriodALL:
		return 365 * 24 * time.Hour
	default:
		return 30 * 24 * time.Hour
	}
}
