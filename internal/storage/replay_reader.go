package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ReplayReader struct {
	db *pgxpool.Pool
}

func NewReplayReader(db *pgxpool.Pool) *ReplayReader {
	return &ReplayReader{db: db}
}

type ReplayEvent struct {
	ID           int64       `json:"id"`
	EventType    string      `json:"event_type"`
	Timestamp    time.Time   `json:"timestamp"`
	VenueID      *uuid.UUID  `json:"venue_id,omitempty"`
	InstrumentID *uuid.UUID  `json:"instrument_id,omitempty"`
	Payload      interface{} `json:"payload"`
}

type ReplaySequence struct {
	Events     []*ReplayEvent `json:"events"`
	TotalCount int            `json:"total_count"`
	StartTime  time.Time      `json:"start_time"`
	EndTime    time.Time      `json:"end_time"`
}

func (r *ReplayReader) ReadMarketEvents(ctx context.Context, startTime, endTime time.Time, venueID, instrumentID *uuid.UUID, limit int) (*ReplaySequence, error) {
	query := `
		SELECT id, event_type, receive_timestamp, venue_id, venue_instrument_id, payload
		FROM raw_market_events
		WHERE receive_timestamp >= $1 AND receive_timestamp <= $2`

	args := []interface{}{startTime, endTime}
	argIndex := 3

	if venueID != nil {
		query += fmt.Sprintf(` AND venue_id = $%d`, argIndex)
		args = append(args, *venueID)
		argIndex++
	}

	if instrumentID != nil {
		query += fmt.Sprintf(` AND venue_instrument_id = $%d`, argIndex)
		args = append(args, *instrumentID)
		argIndex++
	}

	query += fmt.Sprintf(` ORDER BY receive_timestamp ASC LIMIT $%d`, argIndex)
	args = append(args, limit)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]*ReplayEvent, 0)
	for rows.Next() {
		event := &ReplayEvent{}
		var payload []byte

		err := rows.Scan(
			&event.ID, &event.EventType, &event.Timestamp,
			&event.VenueID, &event.InstrumentID, &payload,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return nil, err
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &ReplaySequence{
		Events:     events,
		TotalCount: len(events),
		StartTime:  startTime,
		EndTime:    endTime,
	}, nil
}

func (r *ReplayReader) ReadTrades(ctx context.Context, startTime, endTime time.Time, venueID, instrumentID *uuid.UUID, limit int) (*ReplaySequence, error) {
	query := `
		SELECT id, venue_id, instrument_id, receive_timestamp, price, quantity, side
		FROM market_trades
		WHERE receive_timestamp >= $1 AND receive_timestamp <= $2`

	args := []interface{}{startTime, endTime}
	argIndex := 3

	if venueID != nil {
		query += fmt.Sprintf(` AND venue_id = $%d`, argIndex)
		args = append(args, *venueID)
		argIndex++
	}

	if instrumentID != nil {
		query += fmt.Sprintf(` AND instrument_id = $%d`, argIndex)
		args = append(args, *instrumentID)
		argIndex++
	}

	query += fmt.Sprintf(` ORDER BY receive_timestamp ASC LIMIT $%d`, argIndex)
	args = append(args, limit)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]*ReplayEvent, 0)
	for rows.Next() {
		event := &ReplayEvent{}
		var price, quantity, side string

		err := rows.Scan(
			&event.ID, &event.VenueID, &event.InstrumentID, &event.Timestamp,
			&price, &quantity, &side,
		)
		if err != nil {
			return nil, err
		}

		event.EventType = "TRADE"
		event.Payload = map[string]interface{}{
			"price":    price,
			"quantity": quantity,
			"side":     side,
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &ReplaySequence{
		Events:     events,
		TotalCount: len(events),
		StartTime:  startTime,
		EndTime:    endTime,
	}, nil
}

func (r *ReplayReader) ReadOrderBookSnapshots(ctx context.Context, startTime, endTime time.Time, venueID, instrumentID *uuid.UUID, limit int) (*ReplaySequence, error) {
	query := `
		SELECT id, venue_id, instrument_id, sequence, timestamp, bids, asks
		FROM orderbook_snapshots
		WHERE timestamp >= $1 AND timestamp <= $2`

	args := []interface{}{startTime, endTime}
	argIndex := 3

	if venueID != nil {
		query += fmt.Sprintf(` AND venue_id = $%d`, argIndex)
		args = append(args, *venueID)
		argIndex++
	}

	if instrumentID != nil {
		query += fmt.Sprintf(` AND instrument_id = $%d`, argIndex)
		args = append(args, *instrumentID)
		argIndex++
	}

	query += fmt.Sprintf(` ORDER BY timestamp ASC LIMIT $%d`, argIndex)
	args = append(args, limit)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]*ReplayEvent, 0)
	for rows.Next() {
		event := &ReplayEvent{}
		var sequence int64
		var bids, asks []byte

		err := rows.Scan(
			&event.ID, &event.VenueID, &event.InstrumentID, &sequence,
			&event.Timestamp, &bids, &asks,
		)
		if err != nil {
			return nil, err
		}

		event.EventType = "ORDERBOOK_SNAPSHOT"
		event.Payload = map[string]interface{}{
			"sequence": sequence,
			"bids":     bids,
			"asks":     asks,
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &ReplaySequence{
		Events:     events,
		TotalCount: len(events),
		StartTime:  startTime,
		EndTime:    endTime,
	}, nil
}

func (r *ReplayReader) ReadDecisions(ctx context.Context, startTime, endTime time.Time, strategyInstanceID *uuid.UUID, limit int) (*ReplaySequence, error) {
	query := `
		SELECT id, opportunity_id, strategy_instance_id, decision, reason_code,
			decision_timestamp, reason_detail
		FROM strategy_decisions
		WHERE decision_timestamp >= $1 AND decision_timestamp <= $2`

	args := []interface{}{startTime, endTime}
	argIndex := 3

	if strategyInstanceID != nil {
		query += fmt.Sprintf(` AND strategy_instance_id = $%d`, argIndex)
		args = append(args, *strategyInstanceID)
		argIndex++
	}

	query += fmt.Sprintf(` ORDER BY decision_timestamp ASC LIMIT $%d`, argIndex)
	args = append(args, limit)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]*ReplayEvent, 0)
	for rows.Next() {
		event := &ReplayEvent{}
		var opportunityID, strategyInstanceIDStr string
		var decision, reasonCode string
		var reasonDetail []byte

		err := rows.Scan(
			&event.ID, &opportunityID, &strategyInstanceIDStr,
			&decision, &reasonCode,
			&event.Timestamp, &reasonDetail,
		)
		if err != nil {
			return nil, err
		}

		event.EventType = "STRATEGY_DECISION"
		event.Payload = map[string]interface{}{
			"opportunity_id":       opportunityID,
			"strategy_instance_id": strategyInstanceIDStr,
			"decision":             decision,
			"reason_code":          reasonCode,
			"reason_detail":        reasonDetail,
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &ReplaySequence{
		Events:     events,
		TotalCount: len(events),
		StartTime:  startTime,
		EndTime:    endTime,
	}, nil
}
