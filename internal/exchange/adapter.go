package exchange

import (
	"context"
	"time"
)

// Adapter defines the interface for exchange adapters
type Adapter interface {
	// GetVenueCode returns the venue code this adapter connects to
	GetVenueCode() string

	// GetSymbols returns all available symbols from the venue
	GetSymbols(ctx context.Context) ([]*SymbolInfo, error)

	// GetSymbolInfo returns detailed information about a specific symbol
	GetSymbolInfo(ctx context.Context, symbol string) (*SymbolInfo, error)

	// SubscribeTicker subscribes to ticker updates for a symbol
	SubscribeTicker(ctx context.Context, symbol string) (<-chan *TickerEvent, error)

	// SubscribeOrderBook subscribes to orderbook updates for a symbol
	SubscribeOrderBook(ctx context.Context, symbol string, depth int) (<-chan *OrderBookEvent, error)

	// SubscribeTrades subscribes to trade updates for a symbol
	SubscribeTrades(ctx context.Context, symbol string) (<-chan *TradeEvent, error)

	// SubscribeFunding subscribes to funding rate updates (perp only)
	SubscribeFunding(ctx context.Context, symbol string) (<-chan *FundingEvent, error)

	// Disconnect gracefully disconnects from the venue
	Disconnect(ctx context.Context) error

	// IsConnected returns true if the adapter is connected
	IsConnected() bool
}

// SymbolInfo contains normalized symbol information from a venue
type SymbolInfo struct {
	VenueSymbol      string    `json:"venue_symbol"`
	BaseAsset        string    `json:"base_asset"`
	QuoteAsset       string    `json:"quote_asset"`
	InstrumentType   string    `json:"instrument_type"`
	ContractType     string    `json:"contract_type"`
	ContractSize     *string   `json:"contract_size,omitempty"`
	PriceTick        string    `json:"price_tick"`
	QuantityStep     string    `json:"quantity_step"`
	MinQuantity      *string   `json:"min_quantity,omitempty"`
	MinNotional      *string   `json:"min_notional,omitempty"`
	MarginAsset      *string   `json:"margin_asset,omitempty"`
	SettlementAsset  *string   `json:"settlement_asset,omitempty"`
	IsTradable       bool      `json:"is_tradable"`
	DiscoveredAt     time.Time `json:"discovered_at"`
}

// TickerEvent represents a normalized ticker update
type TickerEvent struct {
	VenueSymbol string    `json:"venue_symbol"`
	BestBid     string    `json:"best_bid"`
	BestAsk     string    `json:"best_ask"`
	LastPrice   string    `json:"last_price"`
	Volume24h   string    `json:"volume_24h"`
	Timestamp   time.Time `json:"timestamp"`
}

// OrderBookEvent represents a normalized orderbook update
type OrderBookEvent struct {
	VenueSymbol string      `json:"venue_symbol"`
	Bids        []PriceLevel `json:"bids"`
	Asks        []PriceLevel `json:"asks"`
	Timestamp   time.Time   `json:"timestamp"`
}

// PriceLevel represents a price level in the orderbook
type PriceLevel struct {
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
}

// TradeEvent represents a normalized trade update
type TradeEvent struct {
	VenueSymbol string    `json:"venue_symbol"`
	Price       string    `json:"price"`
	Quantity    string    `json:"quantity"`
	Side        string    `json:"side"`
	Timestamp   time.Time `json:"timestamp"`
}

// FundingEvent represents a normalized funding rate update (perp only)
type FundingEvent struct {
	VenueSymbol   string    `json:"venue_symbol"`
	FundingRate   string    `json:"funding_rate"`
	NextFundingAt time.Time `json:"next_funding_at"`
	Timestamp     time.Time `json:"timestamp"`
}

// AdapterConfig contains configuration for creating an adapter
type AdapterConfig struct {
	VenueCode    string            `json:"venue_code"`
	APIKey       string            `json:"api_key"`
	APISecret    string            `json:"api_secret"`
	SubaccountID *string           `json:"subaccount_id,omitempty"`
	Testnet      bool              `json:"testnet"`
	Extra        map[string]string `json:"extra,omitempty"`
}
