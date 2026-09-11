package collector

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qwerty7415963/go_be_arbitrage/internal/exchange"
	"github.com/qwerty7415963/go_be_arbitrage/internal/logger"
	"github.com/qwerty7415963/go_be_arbitrage/internal/venue"
)

// CacheInvalidator is called when new funding data is collected
type CacheInvalidator interface {
	Invalidate(venueID uuid.UUID)
}

// Collector fetches funding data from exchanges and stores in database
type Collector struct {
	db          *pgxpool.Pool
	venueRepo   *venue.Repository
	logger      *logger.Logger
	interval    time.Duration
	invalidator CacheInvalidator
	mu          sync.Mutex
	running     bool
}

func NewCollector(
	db *pgxpool.Pool,
	venueRepo *venue.Repository,
	log *logger.Logger,
	interval time.Duration,
	invalidator CacheInvalidator,
) *Collector {
	return &Collector{
		db:          db,
		venueRepo:   venueRepo,
		logger:      log,
		interval:    interval,
		invalidator: invalidator,
	}
}

// Start begins the background collection loop
func (c *Collector) Start(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()

	c.logger.Info("starting funding collector", "interval", c.interval)

	// Run immediately on start
	c.collectAll(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("stopping funding collector")
			return
		case <-ticker.C:
			c.collectAll(ctx)
		}
	}
}

// Stop stops the collector
func (c *Collector) Stop() {
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
}

func (c *Collector) collectAll(ctx context.Context) {
	c.logger.Debug("collecting funding data from all venues")

	var wg sync.WaitGroup

	// Collect from Binance
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.collectBinance(ctx)
	}()

	// Collect from Extended
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.collectExtended(ctx)
	}()

	// Collect from Variational
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.collectVariational(ctx)
	}()

	wg.Wait()
	c.logger.Debug("funding collection complete")
}

func (c *Collector) collectBinance(ctx context.Context) {
	v, err := c.venueRepo.GetByCode(ctx, "binance")
	if err != nil {
		c.logger.Error("failed to get binance venue", "error", err)
		return
	}

	adapter := exchange.NewBinanceAdapter()
	fundingData, err := adapter.FetchAllFunding(ctx)
	if err != nil {
		c.logger.Error("failed to fetch binance funding", "error", err)
		return
	}

	c.logger.Info("collected binance funding", "count", len(fundingData))

	stored := 0
	for _, data := range fundingData {
		baseAsset := data.BaseAsset
		quoteAsset := data.QuoteAsset
		if baseAsset == "" {
			baseAsset = NormalizeBaseAsset(data.Symbol, "binance")
		}
		if quoteAsset == "" {
			quoteAsset = NormalizeQuoteAsset(data.Symbol, "binance")
		}

		if c.storeFundingWithDiscovery(ctx, v.ID, "binance", data.Symbol, baseAsset, quoteAsset, data.FundingRate, adapter.GetFundingInterval(), data.MarkPrice, data.IndexPrice, data.ObservedAt) {
			stored++
		}
	}

	c.logger.Info("stored binance funding", "stored", stored, "total", len(fundingData))

	// Invalidate cache for this venue
	if c.invalidator != nil {
		c.invalidator.Invalidate(v.ID)
	}
}

func (c *Collector) collectExtended(ctx context.Context) {
	v, err := c.venueRepo.GetByCode(ctx, "extended")
	if err != nil {
		c.logger.Error("failed to get extended venue", "error", err)
		return
	}

	adapter := exchange.NewExtendedAdapter()
	fundingData, err := adapter.FetchAllMarkets(ctx)
	if err != nil {
		c.logger.Error("failed to fetch extended funding", "error", err)
		return
	}

	c.logger.Info("collected extended funding", "count", len(fundingData))

	stored := 0
	for _, data := range fundingData {
		baseAsset := data.BaseAsset
		if baseAsset == "" {
			baseAsset = NormalizeBaseAsset(data.Symbol, "extended")
		}
		quoteAsset := NormalizeQuoteAsset(data.Symbol, "extended")

		if c.storeFundingWithDiscovery(ctx, v.ID, "extended", data.Symbol, baseAsset, quoteAsset, data.FundingRate, adapter.GetFundingInterval(), data.MarkPrice, data.IndexPrice, data.ObservedAt) {
			stored++
		}
	}

	c.logger.Info("stored extended funding", "stored", stored, "total", len(fundingData))

	if c.invalidator != nil {
		c.invalidator.Invalidate(v.ID)
	}
}

func (c *Collector) collectVariational(ctx context.Context) {
	v, err := c.venueRepo.GetByCode(ctx, "variational")
	if err != nil {
		c.logger.Error("failed to get variational venue", "error", err)
		return
	}

	adapter := exchange.NewVariationalAdapter()
	fundingData, err := adapter.FetchAllListings(ctx)
	if err != nil {
		c.logger.Error("failed to fetch variational funding", "error", err)
		return
	}

	c.logger.Info("collected variational funding", "count", len(fundingData))

	stored := 0
	for _, data := range fundingData {
		baseAsset := data.BaseAsset
		if baseAsset == "" {
			baseAsset = NormalizeBaseAsset(data.Symbol, "variational")
		}
		quoteAsset := NormalizeQuoteAsset(data.Symbol, "variational")

		if c.storeFundingWithDiscovery(ctx, v.ID, "variational", data.Symbol, baseAsset, quoteAsset, data.FundingRate, data.IntervalS, data.MarkPrice, "", data.ObservedAt) {
			stored++
		}
	}

	c.logger.Info("stored variational funding", "stored", stored, "total", len(fundingData))

	if c.invalidator != nil {
		c.invalidator.Invalidate(v.ID)
	}
}

// ensureInstrument finds or creates an instrument + venue_instrument mapping.
// Returns instrument_id.
func (c *Collector) ensureInstrument(ctx context.Context, venueID uuid.UUID, venueCode, venueSymbol, baseAsset, quoteAsset string) (uuid.UUID, error) {
	// 1. Check if venue_instrument already exists
	var instrumentID uuid.UUID
	err := c.db.QueryRow(ctx,
		`SELECT instrument_id FROM venue_instruments 
		 WHERE venue_id = $1 AND venue_symbol = $2 AND status = 'ACTIVE'`,
		venueID, venueSymbol,
	).Scan(&instrumentID)

	if err == nil {
		return instrumentID, nil
	}

	// 2. Normalize quote asset for canonical symbol matching
	// USDT, USD, BUSD, USDC are all stablecoins → treat as "USD" for canonical matching
	normalizedQuote := quoteAsset
	switch quoteAsset {
	case "USDT", "BUSD", "USDC":
		normalizedQuote = "USD"
	}

	// 3. Build canonical symbol (e.g., "BTCUSD", "SOLUSD")
	canonicalSymbol := baseAsset + normalizedQuote

	// 4. Find or create instrument by canonical_symbol
	err = c.db.QueryRow(ctx,
		`SELECT id FROM instruments WHERE canonical_symbol = $1`,
		canonicalSymbol,
	).Scan(&instrumentID)

	if err != nil {
		// Instrument doesn't exist, create it
		instrumentType := "PERP"
		contractType := "LINEAR"

		err = c.db.QueryRow(ctx,
			`INSERT INTO instruments (canonical_symbol, base_asset, quote_asset, instrument_type, contract_type, price_tick, quantity_step, trading_enabled, discovery_status)
			 VALUES ($1, $2, $3, $4, $5, 0.01, 0.001, false, 'DISCOVERED')
			 RETURNING id`,
			canonicalSymbol, baseAsset, normalizedQuote, instrumentType, contractType,
		).Scan(&instrumentID)

		if err != nil {
			return uuid.Nil, err
		}

		c.logger.Info("auto-discovered new instrument",
			"canonical", canonicalSymbol, "base", baseAsset, "quote", quoteAsset)
	}

	// 4. Create venue_instrument mapping
	_, err = c.db.Exec(ctx,
		`INSERT INTO venue_instruments (venue_id, instrument_id, venue_symbol, status)
		 VALUES ($1, $2, $3, 'ACTIVE')
		 ON CONFLICT DO NOTHING`,
		venueID, instrumentID, venueSymbol,
	)

	if err != nil {
		return uuid.Nil, err
	}

	c.logger.Debug("created venue_instrument mapping",
		"venue", venueCode, "symbol", venueSymbol, "canonical", canonicalSymbol)

	return instrumentID, nil
}

// storeFundingWithDiscovery auto-discovers instruments and stores funding data.
func (c *Collector) storeFundingWithDiscovery(
	ctx context.Context,
	venueID uuid.UUID,
	venueCode string,
	venueSymbol string,
	baseAsset string,
	quoteAsset string,
	fundingRate string,
	intervalSeconds int,
	markPrice string,
	indexPrice string,
	observedAt time.Time,
) bool {
	instrumentID, err := c.ensureInstrument(ctx, venueID, venueCode, venueSymbol, baseAsset, quoteAsset)
	if err != nil {
		c.logger.Debug("failed to ensure instrument",
			"venue", venueCode, "symbol", venueSymbol, "error", err)
		return false
	}

	// Insert funding rate
	_, err = c.db.Exec(ctx,
		`INSERT INTO funding_rates (venue_id, instrument_id, observed_at, funding_rate, interval_seconds, mark_price, index_price)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		venueID, instrumentID, observedAt, fundingRate, intervalSeconds, markPrice, indexPrice,
	)

	if err != nil {
		c.logger.Error("failed to insert funding rate",
			"venue_id", venueID, "symbol", venueSymbol, "error", err)
		return false
	}

	c.logger.Debug("stored funding rate",
		"venue", venueCode, "symbol", venueSymbol, "rate", fundingRate)
	return true
}
