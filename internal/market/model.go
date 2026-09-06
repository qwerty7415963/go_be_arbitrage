package market

import (
	"time"

	"github.com/google/uuid"
)

type EventType string

const (
	EventTypeTrade   EventType = "TRADE"
	EventTypeTicker  EventType = "TICKER"
	EventTypeFunding EventType = "FUNDING"
	EventTypeOrderBook EventType = "ORDER_BOOK"
)

type TradeEvent struct {
	ID                int64     `json:"id" db:"id"`
	VenueID           uuid.UUID `json:"venue_id" db:"venue_id"`
	InstrumentID      uuid.UUID `json:"instrument_id" db:"instrument_id"`
	ExchangeTradeID   string    `json:"exchange_trade_id" db:"exchange_trade_id"`
	ExchangeTimestamp *time.Time `json:"exchange_timestamp" db:"exchange_timestamp"`
	ReceiveTimestamp  time.Time `json:"receive_timestamp" db:"receive_timestamp"`
	Price             string    `json:"price" db:"price"`
	Quantity          string    `json:"quantity" db:"quantity"`
	Side              string    `json:"side" db:"side"`
	SequenceNo        *int64    `json:"sequence_no" db:"sequence_no"`
	RawEventID        *int64    `json:"raw_event_id" db:"raw_event_id"`
}

type TickerEvent struct {
	ID                int64     `json:"id" db:"id"`
	VenueID           uuid.UUID `json:"venue_id" db:"venue_id"`
	InstrumentID      uuid.UUID `json:"instrument_id" db:"instrument_id"`
	ExchangeTimestamp *time.Time `json:"exchange_timestamp" db:"exchange_timestamp"`
	ReceiveTimestamp  time.Time `json:"receive_timestamp" db:"receive_timestamp"`
	BestBidPrice      *string   `json:"best_bid_price" db:"best_bid_price"`
	BestBidQty        *string   `json:"best_bid_qty" db:"best_bid_qty"`
	BestAskPrice      *string   `json:"best_ask_price" db:"best_ask_price"`
	BestAskQty        *string   `json:"best_ask_qty" db:"best_ask_qty"`
	MarkPrice         *string   `json:"mark_price" db:"mark_price"`
	IndexPrice        *string   `json:"index_price" db:"index_price"`
	SequenceNo        *int64    `json:"sequence_no" db:"sequence_no"`
}

type FundingEvent struct {
	ID               int64     `json:"id" db:"id"`
	VenueID          uuid.UUID `json:"venue_id" db:"venue_id"`
	InstrumentID     uuid.UUID `json:"instrument_id" db:"instrument_id"`
	ObservedAt       time.Time `json:"observed_at" db:"observed_at"`
	FundingRate      string    `json:"funding_rate" db:"funding_rate"`
	IntervalSeconds  int       `json:"interval_seconds" db:"interval_seconds"`
	NextFundingAt    *time.Time `json:"next_funding_at" db:"next_funding_at"`
	PremiumRate      *string   `json:"premium_rate" db:"premium_rate"`
	MarkPrice        *string   `json:"mark_price" db:"mark_price"`
	IndexPrice       *string   `json:"index_price" db:"index_price"`
	SourceEventID    *int64    `json:"source_event_id" db:"source_event_id"`
}

type RawMarketEvent struct {
	ID                 int64     `json:"id" db:"id"`
	VenueID            uuid.UUID `json:"venue_id" db:"venue_id"`
	VenueInstrumentID  *uuid.UUID `json:"venue_instrument_id" db:"venue_instrument_id"`
	EventType          EventType `json:"event_type" db:"event_type"`
	ExchangeTimestamp  *time.Time `json:"exchange_timestamp" db:"exchange_timestamp"`
	ReceiveTimestamp   time.Time `json:"receive_timestamp" db:"receive_timestamp"`
	ProcessTimestamp   *time.Time `json:"process_timestamp" db:"process_timestamp"`
	ExchangeSequence   *int64    `json:"exchange_sequence" db:"exchange_sequence"`
	ConnectionID       uuid.UUID `json:"connection_id" db:"connection_id"`
	Payload            []byte    `json:"payload" db:"payload"`
	PayloadHash        string    `json:"payload_hash" db:"payload_hash"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
}

type PriceLevel struct {
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
}

type OrderBookEvent struct {
	VenueSymbol string       `json:"venue_symbol"`
	Bids        []PriceLevel `json:"bids"`
	Asks        []PriceLevel `json:"asks"`
	Timestamp   time.Time    `json:"timestamp"`
}
