package market

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type NormalizedEventRepository struct {
	db *pgxpool.Pool
}

func NewNormalizedEventRepository(db *pgxpool.Pool) *NormalizedEventRepository {
	return &NormalizedEventRepository{db: db}
}

func (r *NormalizedEventRepository) CreateTrade(ctx context.Context, trade *TradeEvent) error {
	query := `
		INSERT INTO market_trades (
			venue_id, instrument_id, exchange_trade_id, exchange_timestamp, 
			receive_timestamp, price, quantity, side, sequence_no, raw_event_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`

	return r.db.QueryRow(ctx, query,
		trade.VenueID, trade.InstrumentID, trade.ExchangeTradeID,
		trade.ExchangeTimestamp, trade.ReceiveTimestamp, trade.Price,
		trade.Quantity, trade.Side, trade.SequenceNo, trade.RawEventID,
	).Scan(&trade.ID)
}

func (r *NormalizedEventRepository) GetTradeByID(ctx context.Context, id int64) (*TradeEvent, error) {
	query := `
		SELECT id, venue_id, instrument_id, exchange_trade_id, exchange_timestamp, 
			receive_timestamp, price, quantity, side, sequence_no, raw_event_id
		FROM market_trades
		WHERE id = $1`

	trade := &TradeEvent{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&trade.ID, &trade.VenueID, &trade.InstrumentID, &trade.ExchangeTradeID,
		&trade.ExchangeTimestamp, &trade.ReceiveTimestamp, &trade.Price,
		&trade.Quantity, &trade.Side, &trade.SequenceNo, &trade.RawEventID,
	)
	if err != nil {
		return nil, err
	}
	return trade, nil
}

func (r *NormalizedEventRepository) ListTradesByVenueInstrument(ctx context.Context, venueID, instrumentID uuid.UUID, limit int) ([]*TradeEvent, error) {
	query := `
		SELECT id, venue_id, instrument_id, exchange_trade_id, exchange_timestamp, 
			receive_timestamp, price, quantity, side, sequence_no, raw_event_id
		FROM market_trades
		WHERE venue_id = $1 AND instrument_id = $2
		ORDER BY receive_timestamp DESC
		LIMIT $3`

	rows, err := r.db.Query(ctx, query, venueID, instrumentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trades []*TradeEvent
	for rows.Next() {
		trade := &TradeEvent{}
		err := rows.Scan(
			&trade.ID, &trade.VenueID, &trade.InstrumentID, &trade.ExchangeTradeID,
			&trade.ExchangeTimestamp, &trade.ReceiveTimestamp, &trade.Price,
			&trade.Quantity, &trade.Side, &trade.SequenceNo, &trade.RawEventID,
		)
		if err != nil {
			return nil, err
		}
		trades = append(trades, trade)
	}
	return trades, rows.Err()
}

func (r *NormalizedEventRepository) CreateTicker(ctx context.Context, ticker *TickerEvent) error {
	query := `
		INSERT INTO market_tickers (
			venue_id, instrument_id, exchange_timestamp, receive_timestamp, 
			best_bid_price, best_bid_qty, best_ask_price, best_ask_qty, 
			mark_price, index_price, sequence_no
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id`

	return r.db.QueryRow(ctx, query,
		ticker.VenueID, ticker.InstrumentID, ticker.ExchangeTimestamp,
		ticker.ReceiveTimestamp, ticker.BestBidPrice, ticker.BestBidQty,
		ticker.BestAskPrice, ticker.BestAskQty, ticker.MarkPrice,
		ticker.IndexPrice, ticker.SequenceNo,
	).Scan(&ticker.ID)
}

func (r *NormalizedEventRepository) GetTickerByID(ctx context.Context, id int64) (*TickerEvent, error) {
	query := `
		SELECT id, venue_id, instrument_id, exchange_timestamp, receive_timestamp, 
			best_bid_price, best_bid_qty, best_ask_price, best_ask_qty, 
			mark_price, index_price, sequence_no
		FROM market_tickers
		WHERE id = $1`

	ticker := &TickerEvent{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&ticker.ID, &ticker.VenueID, &ticker.InstrumentID, &ticker.ExchangeTimestamp,
		&ticker.ReceiveTimestamp, &ticker.BestBidPrice, &ticker.BestBidQty,
		&ticker.BestAskPrice, &ticker.BestAskQty, &ticker.MarkPrice,
		&ticker.IndexPrice, &ticker.SequenceNo,
	)
	if err != nil {
		return nil, err
	}
	return ticker, nil
}

func (r *NormalizedEventRepository) GetLatestTicker(ctx context.Context, venueID, instrumentID uuid.UUID) (*TickerEvent, error) {
	query := `
		SELECT id, venue_id, instrument_id, exchange_timestamp, receive_timestamp, 
			best_bid_price, best_bid_qty, best_ask_price, best_ask_qty, 
			mark_price, index_price, sequence_no
		FROM market_tickers
		WHERE venue_id = $1 AND instrument_id = $2
		ORDER BY receive_timestamp DESC
		LIMIT 1`

	ticker := &TickerEvent{}
	err := r.db.QueryRow(ctx, query, venueID, instrumentID).Scan(
		&ticker.ID, &ticker.VenueID, &ticker.InstrumentID, &ticker.ExchangeTimestamp,
		&ticker.ReceiveTimestamp, &ticker.BestBidPrice, &ticker.BestBidQty,
		&ticker.BestAskPrice, &ticker.BestAskQty, &ticker.MarkPrice,
		&ticker.IndexPrice, &ticker.SequenceNo,
	)
	if err != nil {
		return nil, err
	}
	return ticker, nil
}

func (r *NormalizedEventRepository) CreateFunding(ctx context.Context, funding *FundingEvent) error {
	query := `
		INSERT INTO funding_rates (
			venue_id, instrument_id, observed_at, funding_rate, 
			interval_seconds, next_funding_at, premium_rate, 
			mark_price, index_price, source_event_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`

	return r.db.QueryRow(ctx, query,
		funding.VenueID, funding.InstrumentID, funding.ObservedAt,
		funding.FundingRate, funding.IntervalSeconds, funding.NextFundingAt,
		funding.PremiumRate, funding.MarkPrice, funding.IndexPrice,
		funding.SourceEventID,
	).Scan(&funding.ID)
}

func (r *NormalizedEventRepository) GetFundingByID(ctx context.Context, id int64) (*FundingEvent, error) {
	query := `
		SELECT id, venue_id, instrument_id, observed_at, funding_rate, 
			interval_seconds, next_funding_at, premium_rate, 
			mark_price, index_price, source_event_id
		FROM funding_rates
		WHERE id = $1`

	funding := &FundingEvent{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&funding.ID, &funding.VenueID, &funding.InstrumentID, &funding.ObservedAt,
		&funding.FundingRate, &funding.IntervalSeconds, &funding.NextFundingAt,
		&funding.PremiumRate, &funding.MarkPrice, &funding.IndexPrice,
		&funding.SourceEventID,
	)
	if err != nil {
		return nil, err
	}
	return funding, nil
}

func (r *NormalizedEventRepository) GetLatestFunding(ctx context.Context, venueID, instrumentID uuid.UUID) (*FundingEvent, error) {
	query := `
		SELECT id, venue_id, instrument_id, observed_at, funding_rate, 
			interval_seconds, next_funding_at, premium_rate, 
			mark_price, index_price, source_event_id
		FROM funding_rates
		WHERE venue_id = $1 AND instrument_id = $2
		ORDER BY observed_at DESC
		LIMIT 1`

	funding := &FundingEvent{}
	err := r.db.QueryRow(ctx, query, venueID, instrumentID).Scan(
		&funding.ID, &funding.VenueID, &funding.InstrumentID, &funding.ObservedAt,
		&funding.FundingRate, &funding.IntervalSeconds, &funding.NextFundingAt,
		&funding.PremiumRate, &funding.MarkPrice, &funding.IndexPrice,
		&funding.SourceEventID,
	)
	if err != nil {
		return nil, err
	}
	return funding, nil
}
