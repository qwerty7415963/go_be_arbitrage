package unifiedstate

import (
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Engine struct {
	instruments map[uuid.UUID]*InstrumentState
	venueStates map[string]*VenueMarketState
	mu          sync.RWMutex
}

func NewEngine() *Engine {
	return &Engine{
		instruments: make(map[uuid.UUID]*InstrumentState),
		venueStates: make(map[string]*VenueMarketState),
	}
}

func (e *Engine) GetVenueStateKey(venueID, instrumentID uuid.UUID) string {
	return venueID.String() + ":" + instrumentID.String()
}

func (e *Engine) GetOrCreateVenueState(venueID, instrumentID uuid.UUID, venueCode string) *VenueMarketState {
	key := e.GetVenueStateKey(venueID, instrumentID)

	e.mu.Lock()
	defer e.mu.Unlock()

	if vs, ok := e.venueStates[key]; ok {
		return vs
	}

	vs := &VenueMarketState{
		VenueID:      venueID,
		InstrumentID: instrumentID,
		VenueCode:    venueCode,
		Health:       VenueHealthUnknown,
	}

	e.venueStates[key] = vs
	return vs
}

func (e *Engine) GetVenueState(venueID, instrumentID uuid.UUID) *VenueMarketState {
	key := e.GetVenueStateKey(venueID, instrumentID)

	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.venueStates[key]
}

func (e *Engine) GetOrCreateInstrumentState(instrumentID uuid.UUID, canonicalSymbol, baseAsset, quoteAsset string) *InstrumentState {
	e.mu.Lock()
	defer e.mu.Unlock()

	if is, ok := e.instruments[instrumentID]; ok {
		return is
	}

	is := &InstrumentState{
		ID:              uuid.New(),
		InstrumentID:    instrumentID,
		CanonicalSymbol: canonicalSymbol,
		BaseAsset:       baseAsset,
		QuoteAsset:      quoteAsset,
		VenueStates:     make(map[uuid.UUID]*VenueMarketState),
	}

	e.instruments[instrumentID] = is
	return is
}

func (e *Engine) GetInstrumentState(instrumentID uuid.UUID) *InstrumentState {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.instruments[instrumentID]
}

func (e *Engine) MergeVenueState(instrumentID uuid.UUID, venueState *VenueMarketState) {
	e.mu.RLock()
	is, ok := e.instruments[instrumentID]
	e.mu.RUnlock()

	if !ok {
		return
	}

	is.mu.Lock()
	defer is.mu.Unlock()

	is.VenueStates[venueState.VenueID] = venueState
	is.TotalVenues = len(is.VenueStates)
	is.LastUpdate = time.Now()

	e.recalculateBestBidAsk(is)
	e.recalculateFunding(is)
	e.recalculateDepth(is)
	e.recalculateHealth(is)
}

func (e *Engine) recalculateBestBidAsk(is *InstrumentState) {
	var bestBid, bestAsk *string
	var bestBidVenue, bestAskVenue *uuid.UUID

	for venueID, vs := range is.VenueStates {
		vs.mu.RLock()
		if vs.BestBid != nil && vs.Health == VenueHealthHealthy {
			if bestBid == nil || priceGreater(*vs.BestBid, *bestBid) {
				bid := *vs.BestBid
				bestBid = &bid
				v := venueID
				bestBidVenue = &v
			}
		}
		if vs.BestAsk != nil && vs.Health == VenueHealthHealthy {
			if bestAsk == nil || priceLess(*vs.BestAsk, *bestAsk) {
				ask := *vs.BestAsk
				bestAsk = &ask
				v := venueID
				bestAskVenue = &v
			}
		}
		vs.mu.RUnlock()
	}

	is.BestBid = bestBid
	is.BestBidVenue = bestBidVenue
	is.BestAsk = bestAsk
	is.BestAskVenue = bestAskVenue

	if bestBid != nil && bestAsk != nil {
		spread := calculateSpread(*bestBid, *bestAsk)
		is.Spread = &spread
	}
}

func (e *Engine) recalculateFunding(is *InstrumentState) {
	var latestFunding *FundingState
	var fundingVenue *uuid.UUID

	for venueID, vs := range is.VenueStates {
		vs.mu.RLock()
		if vs.Funding != nil && vs.Health == VenueHealthHealthy {
			if latestFunding == nil || vs.Funding.ReceivedAt.After(latestFunding.ReceivedAt) {
				funding := *vs.Funding
				latestFunding = &funding
				v := venueID
				fundingVenue = &v
			}
		}
		vs.mu.RUnlock()
	}

	is.Funding = latestFunding
	is.FundingVenue = fundingVenue
}

func (e *Engine) recalculateDepth(is *InstrumentState) {
	allBids := make([]DepthLevel, 0)
	allAsks := make([]DepthLevel, 0)

	for _, vs := range is.VenueStates {
		vs.mu.RLock()
		if vs.Health == VenueHealthHealthy {
			for _, bid := range vs.BidDepth {
				allBids = e.mergeDepthLevel(allBids, bid)
			}
			for _, ask := range vs.AskDepth {
				allAsks = e.mergeDepthLevel(allAsks, ask)
			}
		}
		vs.mu.RUnlock()
	}

	sort.Slice(allBids, func(i, j int) bool {
		return priceGreater(allBids[i].Price, allBids[j].Price)
	})

	sort.Slice(allAsks, func(i, j int) bool {
		return priceLess(allAsks[i].Price, allAsks[j].Price)
	})

	if len(allBids) > 10 {
		allBids = allBids[:10]
	}
	if len(allAsks) > 10 {
		allAsks = allAsks[:10]
	}

	is.BidDepth = allBids
	is.AskDepth = allAsks
}

func (e *Engine) mergeDepthLevel(levels []DepthLevel, newLevel PriceLevel) []DepthLevel {
	for i, l := range levels {
		if l.Price == newLevel.Price {
			levels[i].Quantity = newLevel.Quantity
			levels[i].Notional = calculateNotional(newLevel.Price, newLevel.Quantity)
			return levels
		}
	}
	levels = append(levels, DepthLevel{
		Price:    newLevel.Price,
		Quantity: newLevel.Quantity,
		Notional: calculateNotional(newLevel.Price, newLevel.Quantity),
	})
	return levels
}

func (e *Engine) recalculateHealth(is *InstrumentState) {
	healthy := 0
	for _, vs := range is.VenueStates {
		vs.mu.RLock()
		if vs.Health == VenueHealthHealthy {
			healthy++
		}
		vs.mu.RUnlock()
	}
	is.HealthyVenues = healthy

	is.IsStale = healthy == 0
}

func (e *Engine) ExcludeStaleVenues(staleThreshold time.Duration) []uuid.UUID {
	e.mu.RLock()
	defer e.mu.RUnlock()

	staleVenues := make([]uuid.UUID, 0)

	for key, vs := range e.venueStates {
		vs.mu.RLock()
		age := time.Since(vs.LastUpdate)
		if age > staleThreshold {
			vs.mu.RUnlock()
			vs.UpdateHealth(VenueHealthStale)
			venueID := vs.VenueID
			staleVenues = append(staleVenues, venueID)
			_ = key
		} else {
			if vs.Health == VenueHealthStale || vs.Health == VenueHealthUnknown {
				vs.mu.RUnlock()
				vs.UpdateHealth(VenueHealthHealthy)
			} else {
				vs.mu.RUnlock()
			}
		}
	}

	return staleVenues
}

func (e *Engine) GetExecutableDepth(instrumentID uuid.UUID, depth int) *ExecutableDepth {
	e.mu.RLock()
	is, ok := e.instruments[instrumentID]
	e.mu.RUnlock()

	if !ok {
		return nil
	}

	is.mu.RLock()
	defer is.mu.RUnlock()

	result := &ExecutableDepth{
		Timestamp: time.Now(),
	}

	if is.BestBid != nil {
		bid := *is.BestBid
		result.BestBid = &bid
	}
	if is.BestAsk != nil {
		ask := *is.BestAsk
		result.BestAsk = &ask
	}
	if result.BestBid != nil && result.BestAsk != nil {
		spread := calculateSpread(*result.BestBid, *result.BestAsk)
		result.Spread = &spread
	}

	bidDepth := make([]DepthLevel, 0, depth)
	for i := 0; i < len(is.BidDepth) && i < depth; i++ {
		bidDepth = append(bidDepth, is.BidDepth[i])
	}
	result.BidDepth = bidDepth

	askDepth := make([]DepthLevel, 0, depth)
	for i := 0; i < len(is.AskDepth) && i < depth; i++ {
		askDepth = append(askDepth, is.AskDepth[i])
	}
	result.AskDepth = askDepth

	return result
}

func (e *Engine) GetHealthOverview() *HealthOverview {
	e.mu.RLock()
	defer e.mu.RUnlock()

	overview := &HealthOverview{
		VenueHealth: make(map[uuid.UUID]VenueHealthStatus),
		Timestamp:   time.Now(),
	}

	overview.TotalInstruments = len(e.instruments)

	venueHealthMap := make(map[uuid.UUID]VenueHealthStatus)
	for _, vs := range e.venueStates {
		vs.mu.RLock()
		venueHealthMap[vs.VenueID] = vs.Health
		vs.mu.RUnlock()
	}

	for venueID, health := range venueHealthMap {
		overview.VenueHealth[venueID] = health
		switch health {
		case VenueHealthHealthy:
			overview.HealthyVenues++
		case VenueHealthStale:
			overview.StaleVenues++
		case VenueHealthUnhealthy:
			overview.UnhealthyVenues++
		}
	}

	return overview
}

func (e *Engine) GetSnapshot() *UnifiedStateSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	snapshot := &UnifiedStateSnapshot{
		Instruments:     make(map[uuid.UUID]*InstrumentState),
		TotalInstruments: len(e.instruments),
		Timestamp:       time.Now(),
	}

	for id, is := range e.instruments {
		is.mu.RLock()
		snapshot.Instruments[id] = is
		if is.IsStale {
			snapshot.TotalStale++
		} else {
			snapshot.TotalHealthy++
		}
		is.mu.RUnlock()
	}

	return snapshot
}

func priceGreater(a, b string) bool {
	af, _ := new(big.Float).SetString(a)
	bf, _ := new(big.Float).SetString(b)
	return af.Cmp(bf) > 0
}

func priceLess(a, b string) bool {
	af, _ := new(big.Float).SetString(a)
	bf, _ := new(big.Float).SetString(b)
	return af.Cmp(bf) < 0
}

func calculateSpread(bestBid, bestAsk string) string {
	bid, _ := new(big.Float).SetString(bestBid)
	ask, _ := new(big.Float).SetString(bestAsk)
	spread := new(big.Float).Sub(ask, bid)
	return trimTrailingZeros(spread.Text('f', 8))
}

func calculateNotional(price, quantity string) string {
	p, _ := new(big.Float).SetString(price)
	q, _ := new(big.Float).SetString(quantity)
	notional := new(big.Float).Mul(p, q)
	return trimTrailingZeros(notional.Text('f', 8))
}

func trimTrailingZeros(s string) string {
	for len(s) > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	if s == "" {
		return "0"
	}
	return s
}
