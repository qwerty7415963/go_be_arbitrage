package orderbook

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type BookState string

const (
	BookStateHealthy    BookState = "HEALTHY"
	BookStateStale      BookState = "STALE"
	BookStateDesynced   BookState = "DESYNCED"
	BookStateResyncing  BookState = "RESYNCING"
	BookStateDisconnected BookState = "DISCONNECTED"
)

type Side string

const (
	SideBid Side = "BID"
	SideAsk Side = "ASK"
)

type PriceLevel struct {
	Price    string `json:"price" db:"price"`
	Quantity string `json:"quantity" db:"quantity"`
}

type OrderBookSnapshot struct {
	ID            int64      `json:"id" db:"id"`
	VenueID       uuid.UUID  `json:"venue_id" db:"venue_id"`
	InstrumentID  uuid.UUID  `json:"instrument_id" db:"instrument_id"`
	Sequence      int64      `json:"sequence" db:"sequence"`
	BestBid       *string    `json:"best_bid" db:"best_bid"`
	BestAsk       *string    `json:"best_ask" db:"best_ask"`
	Bids          []PriceLevel `json:"bids" db:"-"`
	Asks          []PriceLevel `json:"asks" db:"-"`
	Timestamp     time.Time  `json:"timestamp" db:"timestamp"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
}

type OrderBookDelta struct {
	ID            int64      `json:"id" db:"id"`
	VenueID       uuid.UUID  `json:"venue_id" db:"venue_id"`
	InstrumentID  uuid.UUID  `json:"instrument_id" db:"instrument_id"`
	FromSequence  int64      `json:"from_sequence" db:"from_sequence"`
	ToSequence    int64      `json:"to_sequence" db:"to_sequence"`
	Bids          []PriceLevel `json:"bids" db:"-"`
	Asks          []PriceLevel `json:"asks" db:"-"`
	Timestamp     time.Time  `json:"timestamp" db:"timestamp"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
}

type OrderBook struct {
	VenueID       uuid.UUID     `json:"venue_id"`
	InstrumentID  uuid.UUID     `json:"instrument_id"`
	Bids          []PriceLevel  `json:"bids"`
	Asks          []PriceLevel  `json:"asks"`
	Sequence      int64         `json:"sequence"`
	State         BookState     `json:"state"`
	LastUpdate    time.Time     `json:"last_update"`
	LastSnapshot  time.Time     `json:"last_snapshot"`
	LastDelta     time.Time     `json:"last_delta"`
	GapCount      int           `json:"gap_count"`
	ResyncCount   int           `json:"resync_count"`
	StaleThreshold  time.Duration `json:"-"`
	SyncThreshold   time.Duration `json:"-"`
	mu            sync.RWMutex   `json:"-"`
}

type OrderBookHealth struct {
	VenueID      uuid.UUID  `json:"venue_id"`
	InstrumentID uuid.UUID  `json:"instrument_id"`
	State        BookState  `json:"state"`
	Sequence     int64      `json:"sequence"`
	LastUpdate   time.Time  `json:"last_update"`
	AgeMs        int64      `json:"age_ms"`
	GapCount     int        `json:"gap_count"`
	ResyncCount  int        `json:"resync_count"`
	IsHealthy    bool       `json:"is_healthy"`
	Message      string     `json:"message,omitempty"`
}

type DepthLevel struct {
	Price    string  `json:"price"`
	Quantity string  `json:"quantity"`
	Notional string  `json:"notional"`
}

type OrderBookDepth struct {
	BestBid      *string      `json:"best_bid"`
	BestAsk      *string      `json:"best_ask"`
	Spread       *string      `json:"spread"`
	BidDepth     []DepthLevel `json:"bid_depth"`
	AskDepth     []DepthLevel `json:"ask_depth"`
	Timestamp    time.Time    `json:"timestamp"`
}

type SubscribeRequest struct {
	VenueID      uuid.UUID `json:"venue_id" binding:"required"`
	InstrumentID uuid.UUID `json:"instrument_id" binding:"required"`
	Depth        int       `json:"depth" binding:"required,min=1,max=20"`
}
