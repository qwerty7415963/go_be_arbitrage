package wallet

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// metricExpr maps filter/sort metric names to snapshot expressions.
var metricExpr = map[string]string{
	"pnl":              "snap.realized_pnl",
	"roi":              "snap.roi",
	"win_rate":         "snap.win_rate",
	"volume":           "snap.volume",
	"trade_count":      "snap.trade_count",
	"avg_position":     "snap.avg_position",
	"avg_leverage":     "snap.avg_leverage",
	"long_short_ratio": "(snap.long_count::float8 / NULLIF(snap.short_count, 0))",
	"last_active":      "snap.last_active_at",
}

const lateralSnap = `
LEFT JOIN LATERAL (
    SELECT s.market, s.realized_pnl, s.roi, s.win_rate, s.volume, s.trade_count,
           s.avg_position, s.avg_leverage, s.long_count, s.short_count,
           s.last_active_at, s.computed_at
    FROM wallet_metric_snapshots s
    WHERE s.wallet_id = w.id
      AND s.timeframe = %[1]s
      AND s.venue_id IS NULL
	AND (COALESCE(array_length(%[2]s::text[], 1), 0) = 0 OR s.market = ANY(%[2]s::text[]))
    ORDER BY (s.market IS NULL) DESC, s.computed_at DESC
    LIMIT 1
) snap ON TRUE`

type argCounter struct {
	args []any
}

func (a *argCounter) add(v any) string {
	a.args = append(a.args, v)
	return fmt.Sprintf("$%d", len(a.args))
}

// buildScanFrom assembles the shared FROM/JOIN/WHERE for the scanner; the
// market enum doubles as the lateral row picker and the outer row filter so
// wallets without a matching market snapshot are excluded entirely. The
// caller's private tag joins on userID (never another user's tag).
func buildScanFrom(f *Filters, groupID *uuid.UUID, userID uuid.UUID) (string, *argCounter) {
	ac := &argCounter{}

	tf := ac.add(snapshotKey(f))
	markets := ac.add(nonNil(f.Market))
	uid := ac.add(userID)

	from := `FROM tracked_wallets w
	LEFT JOIN venues v ON v.id = w.venue_id`
	from += fmt.Sprintf(`
	LEFT JOIN user_wallet_tags t ON t.wallet_id = w.id AND t.user_id = %s`, uid)
	from += fmt.Sprintf(`
	LEFT JOIN user_wallet_watchlist wl ON wl.wallet_id = w.id AND wl.user_id = %s`, uid)
	if groupID != nil {
		g := ac.add(*groupID)
		from += fmt.Sprintf(`
	JOIN group_wallet_members gm ON gm.wallet_id = w.id AND gm.group_id = %s`, g)
	}
	from += fmt.Sprintf(lateralSnap, tf, markets)

	where := []string{"TRUE"}

	if f.Search != "" {
		s := ac.add(f.Search)
		where = append(where, fmt.Sprintf(
			"(w.address ILIKE '%%' || %s || '%%' OR t.tag ILIKE '%%' || %s || '%%')", s, s))
	}
	if len(f.Dex) > 0 {
		d := ac.add(f.Dex)
		where = append(where, fmt.Sprintf("v.code = ANY(%s::text[])", d))
	}
	if len(f.Chain) > 0 {
		c := ac.add(f.Chain)
		where = append(where, fmt.Sprintf("w.chain = ANY(%s::text[])", c))
	}
	if len(f.Market) > 0 {
		// snap.market IS NOT NULL implied: NULL = ANY(...) is NULL → false.
		where = append(where, fmt.Sprintf("snap.market = ANY(%s::text[])", markets))
	}
	// WL-H-06: star filter is caller-scoped through the joined user id.
	if f.Watchlisted != nil {
		if *f.Watchlisted {
			where = append(where, "wl.user_id IS NOT NULL")
		} else {
			where = append(where, "wl.user_id IS NULL")
		}
	}

	for _, nf := range f.Numeric {
		expr := metricExpr[nf.Metric]
		if nf.Metric == "long_short_ratio" {
			where = append(where, "snap.long_count IS NOT NULL")
		} else {
			where = append(where, fmt.Sprintf("%s IS NOT NULL", expr))
		}
		switch nf.Op {
		case OpGT:
			v := ac.add(nf.Lo)
			where = append(where, fmt.Sprintf("%s > %s", expr, v))
		case OpGTE:
			v := ac.add(nf.Lo)
			where = append(where, fmt.Sprintf("%s >= %s", expr, v))
		case OpLT:
			v := ac.add(nf.Lo)
			where = append(where, fmt.Sprintf("%s < %s", expr, v))
		case OpLTE:
			v := ac.add(nf.Lo)
			where = append(where, fmt.Sprintf("%s <= %s", expr, v))
		case OpBetween:
			lo := ac.add(nf.Lo)
			hi := ac.add(*nf.Hi)
			where = append(where, fmt.Sprintf("%s >= %s AND %s <= %s", expr, lo, expr, hi))
		}
	}

	if f.LastActiveFrom != nil || f.LastActiveTo != nil {
		where = append(where, "snap.last_active_at IS NOT NULL")
		if f.LastActiveFrom != nil {
			v := ac.add(f.LastActiveFrom.UTC())
			where = append(where, fmt.Sprintf("snap.last_active_at >= %s", v))
		}
		if f.LastActiveTo != nil {
			v := ac.add(f.LastActiveTo.UTC())
			where = append(where, fmt.Sprintf("snap.last_active_at <= %s", v))
		}
	}

	return from + "\n	WHERE " + strings.Join(where, "\n	  AND "), ac
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// snapshotKey resolves the timeframe slot to read (custom ranges use a
// deterministic key so Phase 3 ingestion can write the same slot).
func snapshotKey(f *Filters) string {
	if f.CustomKey != "" {
		return f.CustomKey
	}
	return f.Timeframe
}

const scanSelect = `
	SELECT w.id, w.chain, w.address, COALESCE(v.code, ''), t.tag, wl.user_id IS NOT NULL,
	       w.first_seen_at, w.last_seen_at,
	       snap.realized_pnl, snap.roi, snap.win_rate, snap.volume, snap.trade_count,
	       snap.avg_position, snap.avg_leverage, snap.long_count, snap.short_count,
	       snap.last_active_at, snap.computed_at
`

const scanSelectGroup = `
	SELECT w.id, w.chain, w.address, COALESCE(v.code, ''), t.tag, wl.user_id IS NOT NULL,
	       w.first_seen_at, w.last_seen_at,
	       snap.realized_pnl, snap.roi, snap.win_rate, snap.volume, snap.trade_count,
	       snap.avg_position, snap.avg_leverage, snap.long_count, snap.short_count,
	       snap.last_active_at, snap.computed_at, gm.added_at
`

// scanPage runs the shared count + ordered page flow; scan maps each row.
func (r *Repository) scanPage(ctx context.Context, selectClause, from string, ac *argCounter, sort *SortSpec, limit, offset int, scan func(pgx.Rows) error) (int64, error) {
	var total int64
	if err := r.db.QueryRow(ctx, "SELECT COUNT(*) "+from, ac.args...).Scan(&total); err != nil {
		return 0, err
	}

	sortExpr := metricExpr[sort.Field]
	dir := "DESC"
	if sort.Order == "asc" {
		dir = "ASC"
	}
	query := selectClause + from + fmt.Sprintf(
		"\n	ORDER BY %s %s NULLS LAST, w.chain ASC, w.address ASC\n	LIMIT %d OFFSET %d",
		sortExpr, dir, limit, offset)

	rows, err := r.db.Query(ctx, query, ac.args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	for rows.Next() {
		if err := scan(rows); err != nil {
			return 0, err
		}
	}
	return total, rows.Err()
}

// ScanWallets returns one page of scanner rows plus the total count.
// Ordering is metric-first with the deterministic (chain, address) tiebreak
// (BR-12); NULL metrics sort last regardless of direction.
func (r *Repository) ScanWallets(ctx context.Context, f *Filters, sort *SortSpec, groupID *uuid.UUID, userID uuid.UUID, limit, offset int) ([]*Wallet, int64, error) {
	from, ac := buildScanFrom(f, groupID, userID)

	wallets := []*Wallet{}
	total, err := r.scanPage(ctx, scanSelect, from, ac, sort, limit, offset, func(rows pgx.Rows) error {
		w, err := scanWalletRow(rows)
		if err != nil {
			return err
		}
		wallets = append(wallets, w)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return wallets, total, nil
}

// ScanGroupWallets is the group-scoped scan returning the unified
// GroupWallet rows (Wallet fields + membership added_at).
func (r *Repository) ScanGroupWallets(ctx context.Context, f *Filters, sort *SortSpec, groupID uuid.UUID, userID uuid.UUID, limit, offset int) ([]*GroupWallet, int64, error) {
	from, ac := buildScanFrom(f, &groupID, userID)

	wallets := []*GroupWallet{}
	total, err := r.scanPage(ctx, scanSelectGroup, from, ac, sort, limit, offset, func(rows pgx.Rows) error {
		w, err := scanGroupWalletRow(rows)
		if err != nil {
			return err
		}
		wallets = append(wallets, w)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return wallets, total, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

// metricScan is the nullable metric column block of a snapshot row: it
// backs both the wallet row (scanWalletRow) and the per-market positions
// query (GetPositions). Column order matches scanSelect after the identity
// columns: pnl, roi, win_rate, volume, trade_count, avg_position,
// avg_leverage, long_count, short_count, last_active_at, computed_at.
type metricScan struct {
	pnl, roi, winR sql.NullFloat64
	vol, avgPos    sql.NullFloat64
	avgLev         sql.NullFloat64
	trades, lngs   sql.NullInt64
	shorts         sql.NullInt64
	lastActive     sql.NullTime
	computedAt     sql.NullTime
}

func (m *metricScan) dests() []any {
	return []any{&m.pnl, &m.roi, &m.winR, &m.vol, &m.trades, &m.avgPos, &m.avgLev,
		&m.lngs, &m.shorts, &m.lastActive, &m.computedAt}
}

// result builds Metrics from the scanned columns. ComputedAt is freshness
// metadata only — it never marks the row as having metrics (BR-07).
func (m *metricScan) result() (*Metrics, bool) {
	mm := &Metrics{}
	hasMetric := false
	if m.pnl.Valid {
		mm.RealizedPnl = &m.pnl.Float64
		hasMetric = true
	}
	if m.roi.Valid {
		mm.Roi = &m.roi.Float64
		hasMetric = true
	}
	if m.winR.Valid {
		mm.WinRate = &m.winR.Float64
		hasMetric = true
	}
	if m.vol.Valid {
		mm.Volume = &m.vol.Float64
		hasMetric = true
	}
	if m.trades.Valid {
		mm.TradeCount = &m.trades.Int64
		hasMetric = true
	}
	if m.avgPos.Valid {
		mm.AvgPosition = &m.avgPos.Float64
		hasMetric = true
	}
	if m.avgLev.Valid {
		mm.AvgLeverage = &m.avgLev.Float64
		hasMetric = true
	}
	if m.lngs.Valid {
		mm.LongCount = &m.lngs.Int64
		hasMetric = true
	}
	if m.shorts.Valid {
		mm.ShortCount = &m.shorts.Int64
		hasMetric = true
	}
	if m.lastActive.Valid {
		t := m.lastActive.Time.UTC()
		mm.LastActiveAt = &t
		hasMetric = true
	}
	if m.computedAt.Valid {
		t := m.computedAt.Time.UTC()
		mm.ComputedAt = &t
	}
	return mm, hasMetric
}

func scanWalletRow(rows rowScanner, extra ...any) (*Wallet, error) {
	var (
		w   Wallet
		tag sql.NullString
		ms  metricScan
	)
	dests := []any{&w.ID, &w.Chain, &w.Address, &w.Dex, &tag, &w.Watchlisted,
		&w.FirstSeenAt, &w.LastSeenAt}
	dests = append(dests, ms.dests()...)
	err := rows.Scan(append(dests, extra...)...)
	if err != nil {
		return nil, err
	}
	if tag.Valid {
		w.Tag = &tag.String
	}
	if m, has := ms.result(); has {
		w.Metrics = m
	}
	return &w, nil
}

// scanGroupWalletRow scans the unified group row (scanSelectGroup column
// order): every Wallet column plus the membership added_at.
func scanGroupWalletRow(rows rowScanner) (*GroupWallet, error) {
	var addedAt time.Time
	w, err := scanWalletRow(rows, &addedAt)
	if err != nil {
		return nil, err
	}
	return &GroupWallet{Wallet: *w, AddedAt: addedAt.UTC()}, nil
}

// GetDetail returns a single wallet, its metrics for the timeframe, the
// caller's own group memberships only (BE-06) and the per-market positions
// breakdown for the same timeframe (POS-*).
func (r *Repository) GetDetail(ctx context.Context, id, userID uuid.UUID, f *Filters) (*WalletDetail, error) {
	from, ac := buildScanFrom(f, nil, userID)
	// id filter (added last → highest $n)
	idArg := ac.add(id)
	query := scanSelect + from + "\n	AND w.id = " + idArg + "\n	LIMIT 1"

	w, err := scanWalletRow(r.db.QueryRow(ctx, query, ac.args...))
	if err != nil {
		return nil, err
	}

	detail := &WalletDetail{Wallet: *w, Memberships: []GroupRef{}, Positions: []Position{}}
	rows, err := r.db.Query(ctx, `
		SELECT g.id, g.name
		FROM user_wallet_groups g
		JOIN group_wallet_members m ON m.group_id = g.id
		WHERE m.wallet_id = $1 AND g.user_id = $2
		ORDER BY g.name ASC`, id, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		g := GroupRef{}
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			return nil, err
		}
		detail.Memberships = append(detail.Memberships, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	positions, err := r.GetPositions(ctx, id, snapshotKey(f))
	if err != nil {
		return nil, err
	}
	detail.Positions = positions
	return detail, nil
}

// GetPositions loads the per-market snapshot breakdown for one
// wallet/timeframe (POS-I-01): rows sorted by pnl desc with NULL pnl last,
// then market asc for a deterministic order. Never returns nil.
func (r *Repository) GetPositions(ctx context.Context, walletID uuid.UUID, timeframe string) ([]Position, error) {
	rows, err := r.db.Query(ctx, `
		SELECT market, realized_pnl, roi, win_rate, volume, trade_count,
		       avg_position, avg_leverage, long_count, short_count,
		       last_active_at, computed_at
		FROM wallet_metric_snapshots
		WHERE wallet_id = $1 AND timeframe = $2
		  AND venue_id IS NULL AND market IS NOT NULL
		ORDER BY realized_pnl DESC NULLS LAST, market ASC`, walletID, timeframe)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	positions := []Position{}
	for rows.Next() {
		var (
			p    Position
			ms   metricScan
			mark string
		)
		dests := append([]any{&mark}, ms.dests()...)
		if err := rows.Scan(dests...); err != nil {
			return nil, err
		}
		p.Market = mark
		m, has := ms.result()
		if has {
			p.Metrics = *m
		}
		positions = append(positions, p)
	}
	return positions, rows.Err()
}

// GetGroupOwner returns the owning user of a group (used for scanner
// ownership checks without importing the walletgroup package).
func (r *Repository) GetGroupOwner(ctx context.Context, groupID uuid.UUID) (uuid.UUID, bool, error) {
	var owner uuid.UUID
	err := r.db.QueryRow(ctx,
		`SELECT user_id FROM user_wallet_groups WHERE id = $1`, groupID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	return owner, true, nil
}

// WalletExists reports whether a tracked wallet exists.
func (r *Repository) WalletExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM tracked_wallets WHERE id = $1)`, id).Scan(&exists)
	return exists, err
}

// UpsertTag sets the caller's private tag for a wallet (TAG-I-01).
func (r *Repository) UpsertTag(ctx context.Context, userID, walletID uuid.UUID, tag string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO user_wallet_tags (user_id, wallet_id, tag, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, wallet_id)
		DO UPDATE SET tag = EXCLUDED.tag, updated_at = NOW()`,
		userID, walletID, tag)
	return err
}

// ClearTag removes the caller's private tag (TAG-I-02). Absent tags are a
// no-op.
func (r *Repository) ClearTag(ctx context.Context, userID, walletID uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM user_wallet_tags WHERE user_id = $1 AND wallet_id = $2`,
		userID, walletID)
	return err
}

// SetWatchlisted stars (on=true) or unstars (on=false) a wallet for one
// caller (WL-I-01/WL-I-03): PK upsert makes repeated stars idempotent,
// unstar deletes the row. Rows are per user, so stars are never shared
// (WL-I-02).
func (r *Repository) SetWatchlisted(ctx context.Context, userID, walletID uuid.UUID, on bool) error {
	if on {
		_, err := r.db.Exec(ctx, `
			INSERT INTO user_wallet_watchlist (user_id, wallet_id)
			VALUES ($1, $2)
			ON CONFLICT (user_id, wallet_id) DO NOTHING`, userID, walletID)
		return err
	}
	_, err := r.db.Exec(ctx,
		`DELETE FROM user_wallet_watchlist WHERE user_id = $1 AND wallet_id = $2`,
		userID, walletID)
	return err
}

// LoadFilterConfig builds the enum validation sets from data (venues,
// observed chains, ingested markets).
func (r *Repository) LoadFilterConfig(ctx context.Context) (*FilterConfig, error) {
	cfg := &FilterConfig{}
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(array_agg(DISTINCT lower(code)), '{}')
		FROM venues WHERE status = 'ACTIVE'`).Scan(&cfg.Dexes)
	if err != nil {
		return nil, err
	}
	if err := r.db.QueryRow(ctx,
		`SELECT COALESCE(array_agg(DISTINCT chain), '{}') FROM tracked_wallets`).Scan(&cfg.Chains); err != nil {
		return nil, err
	}
	if err := r.db.QueryRow(ctx, `
		SELECT COALESCE(array_agg(DISTINCT market), '{}')
		FROM wallet_metric_snapshots WHERE market IS NOT NULL`).Scan(&cfg.Markets); err != nil {
		return nil, err
	}
	return cfg, nil
}
