package orderbook

import (
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Engine struct {
	books map[string]*OrderBook
	mu    sync.RWMutex
}

func NewEngine() *Engine {
	return &Engine{
		books: make(map[string]*OrderBook),
	}
}

func (e *Engine) GetBookKey(venueID, instrumentID uuid.UUID) string {
	return venueID.String() + ":" + instrumentID.String()
}

func (e *Engine) GetOrCreateBook(venueID, instrumentID uuid.UUID, staleThreshold, syncThreshold time.Duration) *OrderBook {
	key := e.GetBookKey(venueID, instrumentID)

	e.mu.Lock()
	defer e.mu.Unlock()

	if book, ok := e.books[key]; ok {
		return book
	}

	book := &OrderBook{
		VenueID:        venueID,
		InstrumentID:   instrumentID,
		Bids:           make([]PriceLevel, 0),
		Asks:           make([]PriceLevel, 0),
		State:          BookStateDisconnected,
		StaleThreshold: staleThreshold,
		SyncThreshold:  syncThreshold,
	}

	e.books[key] = book
	return book
}

func (e *Engine) GetBook(venueID, instrumentID uuid.UUID) *OrderBook {
	key := e.GetBookKey(venueID, instrumentID)

	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.books[key]
}

func (e *Engine) ApplySnapshot(book *OrderBook, snapshot *OrderBookSnapshot) error {
	book.mu.Lock()
	defer book.mu.Unlock()

	book.Bids = make([]PriceLevel, len(snapshot.Bids))
	copy(book.Bids, snapshot.Bids)

	book.Asks = make([]PriceLevel, len(snapshot.Asks))
	copy(book.Asks, snapshot.Asks)

	book.Sequence = snapshot.Sequence
	book.LastSnapshot = time.Now()
	book.LastUpdate = time.Now()

	if book.State == BookStateResyncing || book.State == BookStateDisconnected {
		book.State = BookStateHealthy
		book.GapCount = 0
	}

	sortBook(book)

	return nil
}

func (e *Engine) ApplyDelta(book *OrderBook, delta *OrderBookDelta) error {
	book.mu.Lock()
	defer book.mu.Unlock()

	if delta.FromSequence != book.Sequence {
		book.State = BookStateDesynced
		book.GapCount++
		return ErrSequenceMismatch
	}

	for _, bid := range delta.Bids {
		applyLevel(&book.Bids, bid)
	}

	for _, ask := range delta.Asks {
		applyLevel(&book.Asks, ask)
	}

	book.Sequence = delta.ToSequence
	book.LastDelta = time.Now()
	book.LastUpdate = time.Now()

	if book.State == BookStateDesynced {
		book.State = BookStateHealthy
	}

	sortBook(book)

	return nil
}

func applyLevel(levels *[]PriceLevel, level PriceLevel) {
	if level.Quantity == "0" || level.Quantity == "" {
		for i, existing := range *levels {
			if existing.Price == level.Price {
				*levels = append((*levels)[:i], (*levels)[i+1:]...)
				return
			}
		}
		return
	}

	for i, existing := range *levels {
		if existing.Price == level.Price {
			(*levels)[i].Quantity = level.Quantity
			return
		}
	}

	*levels = append(*levels, level)
}

func sortBook(book *OrderBook) {
	sort.Slice(book.Bids, func(i, j int) bool {
		return priceGreater(book.Bids[i].Price, book.Bids[j].Price)
	})

	sort.Slice(book.Asks, func(i, j int) bool {
		return priceLess(book.Asks[i].Price, book.Asks[j].Price)
	})
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

func (e *Engine) CheckFreshness(book *OrderBook) BookState {
	book.mu.Lock()
	defer book.mu.Unlock()

	if book.State == BookStateDisconnected || book.State == BookStateResyncing {
		return book.State
	}

	age := time.Since(book.LastUpdate)

	if age > book.StaleThreshold {
		book.State = BookStateStale
		return BookStateStale
	}

	return BookStateHealthy
}

func (e *Engine) GetHealth(book *OrderBook) *OrderBookHealth {
	book.mu.RLock()
	defer book.mu.RUnlock()

	age := time.Since(book.LastUpdate)
	state := book.State

	if state != BookStateDisconnected && state != BookStateResyncing {
		if age > book.StaleThreshold {
			state = BookStateStale
		}
	}

	return &OrderBookHealth{
		VenueID:     book.VenueID,
		InstrumentID: book.InstrumentID,
		State:       state,
		Sequence:    book.Sequence,
		LastUpdate:  book.LastUpdate,
		AgeMs:       age.Milliseconds(),
		GapCount:    book.GapCount,
		ResyncCount: book.ResyncCount,
		IsHealthy:   state == BookStateHealthy,
	}
}

func (e *Engine) RequestResync(book *OrderBook) {
	book.mu.Lock()
	defer book.mu.Unlock()

	book.State = BookStateResyncing
	book.ResyncCount++
}

func (e *Engine) GetDepth(book *OrderBook, depth int) *OrderBookDepth {
	book.mu.RLock()
	defer book.mu.RUnlock()

	result := &OrderBookDepth{
		Timestamp: time.Now(),
	}

	if len(book.Bids) > 0 {
		bid := book.Bids[0].Price
		result.BestBid = &bid
	}

	if len(book.Asks) > 0 {
		ask := book.Asks[0].Price
		result.BestAsk = &ask
	}

	if result.BestBid != nil && result.BestAsk != nil {
		spread := calculateSpread(*result.BestBid, *result.BestAsk)
		result.Spread = &spread
	}

	bidDepth := make([]DepthLevel, 0, depth)
	for i := 0; i < len(book.Bids) && i < depth; i++ {
		bidDepth = append(bidDepth, DepthLevel{
			Price:    book.Bids[i].Price,
			Quantity: book.Bids[i].Quantity,
			Notional: calculateNotional(book.Bids[i].Price, book.Bids[i].Quantity),
		})
	}
	result.BidDepth = bidDepth

	askDepth := make([]DepthLevel, 0, depth)
	for i := 0; i < len(book.Asks) && i < depth; i++ {
		askDepth = append(askDepth, DepthLevel{
			Price:    book.Asks[i].Price,
			Quantity: book.Asks[i].Quantity,
			Notional: calculateNotional(book.Asks[i].Price, book.Asks[i].Quantity),
		})
	}
	result.AskDepth = askDepth

	return result
}

func (e *Engine) GetExecutablePrice(book *OrderBook, side Side, quantity string) *string {
	book.mu.RLock()
	defer book.mu.RUnlock()

	var levels []PriceLevel
	if side == SideBid {
		levels = book.Asks
	} else {
		levels = book.Bids
	}

	if len(levels) == 0 {
		return nil
	}

	return &levels[0].Price
}

func (e *Engine) GetBestPrices(book *OrderBook) (bestBid, bestAsk *string) {
	book.mu.RLock()
	defer book.mu.RUnlock()

	if len(book.Bids) > 0 {
		bestBid = &book.Bids[0].Price
	}

	if len(book.Asks) > 0 {
		bestAsk = &book.Asks[0].Price
	}

	return bestBid, bestAsk
}

func (e *Engine) IsTradable(book *OrderBook) bool {
	book.mu.RLock()
	defer book.mu.RUnlock()

	if book.State != BookStateHealthy {
		return false
	}

	if len(book.Bids) == 0 || len(book.Asks) == 0 {
		return false
	}

	age := time.Since(book.LastUpdate)
	if age > book.SyncThreshold {
		return false
	}

	return true
}

func (e *Engine) ResetBook(book *OrderBook) {
	book.mu.Lock()
	defer book.mu.Unlock()

	book.Bids = make([]PriceLevel, 0)
	book.Asks = make([]PriceLevel, 0)
	book.Sequence = 0
	book.State = BookStateResyncing
	book.GapCount = 0
	book.ResyncCount++
}
