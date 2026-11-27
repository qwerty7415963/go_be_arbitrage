package traderfollow

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Follow(ctx context.Context, userID uuid.UUID, items []MemberInput) (int64, int64, error) {
	norm := make([]MemberInput, 0, len(items))
	for _, it := range items {
		it.Venue = strings.ToLower(strings.TrimSpace(it.Venue))
		it.WalletAddress = strings.ToLower(strings.TrimSpace(it.WalletAddress))
		norm = append(norm, it)
	}
	items = norm

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	var added, skipped int64
	for _, it := range items {
		if it.Venue == "" || it.WalletAddress == "" {
			return 0, 0, domain.NewError(domain.ErrCodeValidation, "venue and wallet_address are required")
		}
		// validate venue
		var venueID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM venues WHERE code = $1`, it.Venue).Scan(&venueID); err != nil {
			if err == pgx.ErrNoRows {
				return 0, 0, domain.NewError(domain.ErrCodeValidation, "unknown venue: "+it.Venue)
			}
			return 0, 0, err
		}
		// validate registry
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trader_registry WHERE venue_id=$1 AND wallet_address=$2)`, venueID, it.WalletAddress).Scan(&exists); err != nil {
			return 0, 0, err
		}
		if !exists {
			return 0, 0, domain.NewError(domain.ErrCodeNotFound, "unknown wallet: "+it.WalletAddress)
		}

		notify := true
		if it.NotifyEnabled != nil {
			notify = *it.NotifyEnabled
		}
		var alias *string
		if it.Alias != nil {
			trimmed := strings.TrimSpace(*it.Alias)
			if utf8.RuneCountInString(trimmed) > 100 {
				return 0, 0, domain.NewError(domain.ErrCodeValidation, "alias exceeds 100 characters")
			}
			if trimmed != "" {
				alias = &trimmed
			}
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO trader_follows (user_id, venue_id, wallet_address, notify_enabled, alias, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
			ON CONFLICT (user_id, venue_id, wallet_address) DO NOTHING`,
			userID, venueID, it.WalletAddress, notify, alias)
		if err != nil {
			return 0, 0, err
		}
		if tag.RowsAffected() == 1 {
			added++
		} else {
			skipped++
			sets := []string{}
			args := []any{userID, venueID, it.WalletAddress}
			if it.NotifyEnabled != nil {
				sets = append(sets, fmt.Sprintf("notify_enabled=$%d", len(args)+1))
				args = append(args, notify)
			}
			if alias != nil {
				sets = append(sets, fmt.Sprintf("alias=$%d", len(args)+1))
				args = append(args, alias)
			}
			if len(sets) > 0 {
				sets = append(sets, "updated_at=NOW()")
				_, _ = tx.Exec(ctx, "UPDATE trader_follows SET "+strings.Join(sets, ", ")+" WHERE user_id=$1 AND venue_id=$2 AND wallet_address=$3", args...)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return added, skipped, nil
}

func (r *Repository) Unfollow(ctx context.Context, userID uuid.UUID, items []MemberInput) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var n int64
	for _, it := range items {
		venue := strings.ToLower(strings.TrimSpace(it.Venue))
		addr := strings.ToLower(strings.TrimSpace(it.WalletAddress))
		var venueID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM venues WHERE code=$1`, venue).Scan(&venueID); err != nil {
			if err == pgx.ErrNoRows {
				continue
			}
			return 0, err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM trader_follows WHERE user_id=$1 AND venue_id=$2 AND wallet_address=$3`, userID, venueID, addr)
		if err != nil {
			return 0, err
		}
		n += tag.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return n, nil
}

func (r *Repository) UpdateFollow(ctx context.Context, userID uuid.UUID, venue, addr string, req UpdateFollowRequest) error {
	venue = strings.ToLower(strings.TrimSpace(venue))
	addr = strings.ToLower(strings.TrimSpace(addr))
	if req.NotifyEnabled == nil && req.Alias == nil {
		return domain.NewError(domain.ErrCodeValidation, "at least one of notify_enabled or alias is required")
	}
	var venueID uuid.UUID
	if err := r.pool.QueryRow(ctx, `SELECT id FROM venues WHERE code=$1`, venue).Scan(&venueID); err != nil {
		if err == pgx.ErrNoRows {
			return domain.NewError(domain.ErrCodeValidation, "unknown venue: "+venue)
		}
		return err
	}
	sets := []string{}
	args := []any{userID, venueID, addr}
	if req.NotifyEnabled != nil {
		sets = append(sets, fmt.Sprintf("notify_enabled=$%d", len(args)+1))
		args = append(args, *req.NotifyEnabled)
	}
	if req.Alias != nil {
		trimmed := strings.TrimSpace(*req.Alias)
		if utf8.RuneCountInString(trimmed) > 100 {
			return domain.NewError(domain.ErrCodeValidation, "alias exceeds 100 characters")
		}
		if trimmed == "" {
			sets = append(sets, "alias=NULL")
		} else {
			sets = append(sets, fmt.Sprintf("alias=$%d", len(args)+1))
			args = append(args, trimmed)
		}
	}
	sets = append(sets, "updated_at=NOW()")
	tag, err := r.pool.Exec(ctx, "UPDATE trader_follows SET "+strings.Join(sets, ", ")+" WHERE user_id=$1 AND venue_id=$2 AND wallet_address=$3", args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NewError(domain.ErrCodeNotFound, "follow not found")
	}
	return nil
}

func (r *Repository) Status(ctx context.Context, userID uuid.UUID, venue, addr string) (*FollowStatus, error) {
	venue = strings.ToLower(strings.TrimSpace(venue))
	addr = strings.ToLower(strings.TrimSpace(addr))
	var venueID uuid.UUID
	if err := r.pool.QueryRow(ctx, `SELECT id FROM venues WHERE code=$1`, venue).Scan(&venueID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.NewError(domain.ErrCodeValidation, "unknown venue: "+venue)
		}
		return nil, err
	}
	st := &FollowStatus{InGroups: []GroupRef{}}
	var alias, display *string
	var notify bool
	err := r.pool.QueryRow(ctx, `
		SELECT f.alias, f.notify_enabled, r.display_name
		FROM trader_follows f
		LEFT JOIN trader_registry r ON r.venue_id=f.venue_id AND r.wallet_address=f.wallet_address
		WHERE f.user_id=$1 AND f.venue_id=$2 AND f.wallet_address=$3`, userID, venueID, addr).Scan(&alias, &notify, &display)
	if err == nil {
		st.IsFollowed = true
		st.NotifyEnabled = notify
		st.Alias = alias
	} else if err != pgx.ErrNoRows {
		return nil, err
	}
	if !st.IsFollowed {
		_ = r.pool.QueryRow(ctx, `SELECT display_name FROM trader_registry WHERE venue_id=$1 AND wallet_address=$2`, venueID, addr).Scan(&display)
	}
	st.DisplayName = display
	grows, err := r.pool.Query(ctx, `
		SELECT g.id, g.name
		FROM trader_group_members m
		JOIN trader_groups g ON g.id=m.group_id
		WHERE g.user_id=$1 AND m.venue_id=$2 AND m.wallet_address=$3
		ORDER BY g.name`, userID, venueID, addr)
	if err == nil {
		for grows.Next() {
			var ref GroupRef
			if err := grows.Scan(&ref.ID, &ref.Name); err == nil {
				st.InGroups = append(st.InGroups, ref)
			}
		}
		grows.Close()
	}
	return st, nil
}

func (r *Repository) Keys(ctx context.Context, userID uuid.UUID) ([]FollowKey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT v.code, f.wallet_address, f.alias
		FROM trader_follows f
		JOIN venues v ON v.id = f.venue_id
		WHERE f.user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FollowKey{}
	for rows.Next() {
		var k FollowKey
		if err := rows.Scan(&k.Venue, &k.WalletAddress, &k.Alias); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// inGroupsMap returns set of "venueID|addr" that are in any user group.
func (r *Repository) inGroupsSet(ctx context.Context, userID uuid.UUID) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m.venue_id, m.wallet_address
		FROM trader_group_members m
		JOIN trader_groups g ON g.id = m.group_id
		WHERE g.user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var vid uuid.UUID
		var addr string
		if err := rows.Scan(&vid, &addr); err != nil {
			return nil, err
		}
		out[vid.String()+"|"+strings.ToLower(strings.TrimSpace(addr))] = true
	}
	return out, rows.Err()
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID, params ListFollowsParams) ([]*FollowListItem, int, error) {
	if params.Period == "" {
		params.Period = trader.Period30D
	}
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.Sort == "" {
		params.Sort = "followed_at"
	}
	if params.Dir != "asc" && params.Dir != "desc" {
		params.Dir = "desc"
	}

	// fetch all follows for user
	rows, err := r.pool.Query(ctx, `
		SELECT f.venue_id, v.code, f.wallet_address, r.display_name, f.alias, f.notify_enabled, f.created_at, f.updated_at
		FROM trader_follows f
		JOIN venues v ON v.id = f.venue_id
		LEFT JOIN trader_registry r ON r.venue_id = f.venue_id AND r.wallet_address = f.wallet_address
		WHERE f.user_id = $1`, userID)
	if err != nil {
		return nil, 0, err
	}
	type base struct {
		venueID uuid.UUID
		venue   string
		addr    string
		display *string
		alias   *string
		notify  bool
		created time.Time
		updated time.Time
	}
	bases := []base{}
	for rows.Next() {
		var b base
		if err := rows.Scan(&b.venueID, &b.venue, &b.addr, &b.display, &b.alias, &b.notify, &b.created, &b.updated); err != nil {
			rows.Close()
			return nil, 0, err
		}
		bases = append(bases, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// search
	if strings.TrimSpace(params.Search) != "" {
		needle := strings.ToLower(strings.TrimSpace(params.Search))
		filtered := bases[:0]
		for _, b := range bases {
			if strings.Contains(strings.ToLower(b.addr), needle) {
				filtered = append(filtered, b)
				continue
			}
			if b.display != nil && strings.Contains(strings.ToLower(*b.display), needle) {
				filtered = append(filtered, b)
			}
		}
		bases = filtered
	}

	inGroups, err := r.inGroupsSet(ctx, userID)
	if err != nil {
		return nil, 0, err
	}

	// filter group membership
	if params.Filter == "in_groups" || params.Filter == "not_in_groups" {
		filtered := bases[:0]
		for _, b := range bases {
			key := b.venueID.String() + "|" + strings.ToLower(strings.TrimSpace(b.addr))
			isIn := inGroups[key]
			if params.Filter == "in_groups" && isIn {
				filtered = append(filtered, b)
			}
			if params.Filter == "not_in_groups" && !isIn {
				filtered = append(filtered, b)
			}
		}
		bases = filtered
	}

	// batch period metrics
	periodMetrics := map[string]*trader.PeriodMetrics{}
	if len(bases) > 0 {
		vids := make([]uuid.UUID, 0, len(bases))
		addrs := make([]string, 0, len(bases))
		for _, b := range bases {
			vids = append(vids, b.venueID)
			addrs = append(addrs, b.addr)
		}
		mrows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, pnl, realized_pnl, roi, win_rate, volume, trade_count,
			       profit_factor, max_drawdown_pct, last_trade_at, data_status, is_partial
			FROM trader_period_metrics
			WHERE period = $1 AND (venue_id, wallet_address) IN (SELECT unnest($2::uuid[]), unnest($3::text[]))`,
			params.Period, vids, addrs)
		if err == nil {
			defer mrows.Close()
			for mrows.Next() {
				var vid uuid.UUID
				var addr string
				var pnl, realized, roi, winRate, volume, profitFactor, maxDD *float64
				var tradeCount *int64
				var lastTrade *time.Time
				var status string
				var partial bool
				if err := mrows.Scan(&vid, &addr, &pnl, &realized, &roi, &winRate, &volume, &tradeCount, &profitFactor, &maxDD, &lastTrade, &status, &partial); err == nil {
					pm := &trader.PeriodMetrics{PnL: pnl, RealizedPnL: realized, ROI: roi, WinRate: winRate, Volume: volume, TradeCount: tradeCount, ProfitFactor: profitFactor, MaxDrawdownPct: maxDD, LastTradeAt: lastTrade, DataStatus: trader.DataStatus(status), IsPartial: partial}
					periodMetrics[vid.String()+"|"+strings.ToLower(strings.TrimSpace(addr))] = pm
				}
			}
		}
	}

	// batch summary account value
	summaryAV := map[string]*float64{}
	// batch positions upnl + count
	positionsUPNL := map[string]*float64{}
	positionsCount := map[string]int{}
	if len(bases) > 0 {
		vids := make([]uuid.UUID, 0, len(bases))
		addrs := make([]string, 0, len(bases))
		for _, b := range bases {
			vids = append(vids, b.venueID)
			addrs = append(addrs, b.addr)
		}
		srows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, account_value
			FROM trader_position_summary
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if err == nil {
			defer srows.Close()
			for srows.Next() {
				var vid uuid.UUID
				var addr string
				var av *float64
				if err := srows.Scan(&vid, &addr, &av); err == nil {
					summaryAV[vid.String()+"|"+strings.ToLower(strings.TrimSpace(addr))] = av
				}
			}
		}
		prows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, SUM(unrealized_pnl), COUNT(*)::int
			FROM trader_positions
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))
			GROUP BY venue_id, wallet_address`, vids, addrs)
		if err == nil {
			defer prows.Close()
			for prows.Next() {
				var vid uuid.UUID
				var addr string
				var upnl *float64
				var cnt int
				if err := prows.Scan(&vid, &addr, &upnl, &cnt); err == nil {
					k := vid.String() + "|" + strings.ToLower(strings.TrimSpace(addr))
					positionsUPNL[k] = upnl
					positionsCount[k] = cnt
				}
			}
		}
	}

	items := make([]*FollowListItem, 0, len(bases))
	for _, b := range bases {
		key := b.venueID.String() + "|" + strings.ToLower(strings.TrimSpace(b.addr))
		item := &FollowListItem{
			VenueID:       b.venueID,
			Venue:         b.venue,
			WalletAddress: b.addr,
			DisplayName:   b.display,
			Alias:         b.alias,
			NotifyEnabled: b.notify,
			FollowedAt:    b.created,
			UpdatedAt:     b.updated,
			InGroups:      []string{},
		}
		if pm, ok := periodMetrics[key]; ok && pm != nil {
			item.PnL = pm.PnL
			item.RealizedPnL = pm.RealizedPnL
			item.ROI = pm.ROI
			item.WinRate = pm.WinRate
			item.Volume = pm.Volume
			item.TradeCount = pm.TradeCount
			item.ProfitFactor = pm.ProfitFactor
			item.MaxDrawdown = pm.MaxDrawdownPct
			item.LastTradeAt = pm.LastTradeAt
			item.DataStatus = string(pm.DataStatus)
		} else {
			item.DataStatus = "missing"
		}
		item.AccountValue = summaryAV[key]
		item.UnrealizedPnL = positionsUPNL[key]
		item.OpenPositions = positionsCount[key]

		// in_groups names
		// fetch group names for this wallet
		grows, err := r.pool.Query(ctx, `
			SELECT g.name
			FROM trader_group_members m
			JOIN trader_groups g ON g.id = m.group_id
			WHERE g.user_id = $1 AND m.venue_id = $2 AND m.wallet_address = $3`, userID, b.venueID, b.addr)
		if err == nil {
			for grows.Next() {
				var name string
				if err := grows.Scan(&name); err == nil {
					item.InGroups = append(item.InGroups, name)
				}
			}
			grows.Close()
		}

		// filter missing/stale
		if params.Filter == "missing_data" && item.DataStatus != "missing" {
			continue
		}
		if params.Filter == "stale" && item.DataStatus != "stale" {
			continue
		}

		items = append(items, item)
	}

	// sort
	sort.Slice(items, func(i, j int) bool {
		less := false
		switch params.Sort {
		case "wallet_address":
			less = items[i].WalletAddress < items[j].WalletAddress
		case "followed_at":
			less = items[i].FollowedAt.Before(items[j].FollowedAt)
		case "account_value":
			avI, avJ := 0.0, 0.0
			if items[i].AccountValue != nil {
				avI = *items[i].AccountValue
			}
			if items[j].AccountValue != nil {
				avJ = *items[j].AccountValue
			}
			less = avI < avJ
		case "unrealized_pnl":
			uI, uJ := 0.0, 0.0
			if items[i].UnrealizedPnL != nil {
				uI = *items[i].UnrealizedPnL
			}
			if items[j].UnrealizedPnL != nil {
				uJ = *items[j].UnrealizedPnL
			}
			less = uI < uJ
		case "roi":
			rI, rJ := 0.0, 0.0
			if items[i].ROI != nil {
				rI = *items[i].ROI
			}
			if items[j].ROI != nil {
				rJ = *items[j].ROI
			}
			less = rI < rJ
		case "volume":
			vI, vJ := 0.0, 0.0
			if items[i].Volume != nil {
				vI = *items[i].Volume
			}
			if items[j].Volume != nil {
				vJ = *items[j].Volume
			}
			less = vI < vJ
		case "open_positions":
			less = items[i].OpenPositions < items[j].OpenPositions
		case "last_trade_at":
			// nil last
			if items[i].LastTradeAt == nil {
				less = true
			} else if items[j].LastTradeAt != nil {
				less = items[i].LastTradeAt.Before(*items[j].LastTradeAt)
			}
		case "updated_at":
			less = items[i].UpdatedAt.Before(items[j].UpdatedAt)
		case "pnl":
			pI, pJ := 0.0, 0.0
			if items[i].PnL != nil {
				pI = *items[i].PnL
			}
			if items[j].PnL != nil {
				pJ = *items[j].PnL
			}
			less = pI < pJ
		case "win_rate":
			wI, wJ := 0.0, 0.0
			if items[i].WinRate != nil {
				wI = *items[i].WinRate
			}
			if items[j].WinRate != nil {
				wJ = *items[j].WinRate
			}
			less = wI < wJ
		case "trade_count":
			tI, tJ := int64(0), int64(0)
			if items[i].TradeCount != nil {
				tI = *items[i].TradeCount
			}
			if items[j].TradeCount != nil {
				tJ = *items[j].TradeCount
			}
			less = tI < tJ
		default:
			less = items[i].FollowedAt.Before(items[j].FollowedAt)
		}
		if params.Dir == "desc" {
			return !less
		}
		return less
	})

	total := len(items)
	start := (params.Page - 1) * params.PageSize
	if start > total {
		start = total
	}
	end := start + params.PageSize
	if end > total {
		end = total
	}
	page := items[start:end]
	return page, total, nil
}

func (r *Repository) Overview(ctx context.Context, userID uuid.UUID, period string) (*FollowOverview, error) {
	if period == "" {
		period = trader.Period30D
	}
	// groups
	grows, err := r.pool.Query(ctx, `SELECT id FROM trader_groups WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	groupIDs := []uuid.UUID{}
	for grows.Next() {
		var id uuid.UUID
		if err := grows.Scan(&id); err != nil {
			grows.Close()
			return nil, err
		}
		groupIDs = append(groupIDs, id)
	}
	grows.Close()

	// follows
	frows, err := r.pool.Query(ctx, `
		SELECT venue_id, wallet_address FROM trader_follows WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	followKeys := []struct {
		vid  uuid.UUID
		addr string
	}{}
	for frows.Next() {
		var vid uuid.UUID
		var addr string
		if err := frows.Scan(&vid, &addr); err != nil {
			frows.Close()
			return nil, err
		}
		followKeys = append(followKeys, struct {
			vid  uuid.UUID
			addr string
		}{vid, addr})
	}
	frows.Close()

	// group members
	mrows, err := r.pool.Query(ctx, `
		SELECT m.venue_id, m.wallet_address
		FROM trader_group_members m
		JOIN trader_groups g ON g.id = m.group_id
		WHERE g.user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	groupKeys := []struct {
		vid  uuid.UUID
		addr string
	}{}
	for mrows.Next() {
		var vid uuid.UUID
		var addr string
		if err := mrows.Scan(&vid, &addr); err != nil {
			mrows.Close()
			return nil, err
		}
		groupKeys = append(groupKeys, struct {
			vid  uuid.UUID
			addr string
		}{vid, addr})
	}
	mrows.Close()

	// distinct union
	type key struct {
		vid  uuid.UUID
		addr string
	}
	set := map[key]bool{}
	for _, k := range followKeys {
		set[key{k.vid, strings.ToLower(strings.TrimSpace(k.addr))}] = true
	}
	for _, k := range groupKeys {
		set[key{k.vid, strings.ToLower(strings.TrimSpace(k.addr))}] = true
	}
	overlap := 0
	groupOnly := 0
	followSet := map[key]bool{}
	for _, k := range followKeys {
		followSet[key{k.vid, strings.ToLower(strings.TrimSpace(k.addr))}] = true
	}
	for _, k := range groupKeys {
		gk := key{k.vid, strings.ToLower(strings.TrimSpace(k.addr))}
		if followSet[gk] {
			// counted in overlap once; handle below
		}
	}
	// compute overlap/groupOnly
	groupSet := map[key]bool{}
	for _, k := range groupKeys {
		groupSet[key{k.vid, strings.ToLower(strings.TrimSpace(k.addr))}] = true
	}
	for k := range followSet {
		if groupSet[k] {
			overlap++
		}
	}
	for k := range groupSet {
		if !followSet[k] {
			groupOnly++
		}
	}

	// account value/uPnL union
	vids := make([]uuid.UUID, 0, len(set))
	addrs := make([]string, 0, len(set))
	for k := range set {
		vids = append(vids, k.vid)
		addrs = append(addrs, k.addr)
	}
	var avSum float64
	var avSeen bool
	var upnlSum float64
	var upnlSeen bool
	if len(vids) > 0 {
		srows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, account_value
			FROM trader_position_summary
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if err == nil {
			defer srows.Close()
			for srows.Next() {
				var vid uuid.UUID
				var addr string
				var av *float64
				if err := srows.Scan(&vid, &addr, &av); err == nil && av != nil {
					avSum += *av
					avSeen = true
				}
			}
		}
		prows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, SUM(unrealized_pnl)
			FROM trader_positions
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))
			GROUP BY venue_id, wallet_address`, vids, addrs)
		if err == nil {
			defer prows.Close()
			for prows.Next() {
				var vid uuid.UUID
				var addr string
				var upnl *float64
				if err := prows.Scan(&vid, &addr, &upnl); err == nil && upnl != nil {
					upnlSum += *upnl
					upnlSeen = true
				}
			}
		}
	}

	var avPtr *float64
	if avSeen {
		v := avSum
		avPtr = &v
	}
	var upnlPtr *float64
	if upnlSeen {
		v := upnlSum
		upnlPtr = &v
	}

	// partial: if any follow has missing period metrics
	partial := false
	if len(followKeys) > 0 {
		fvids := make([]uuid.UUID, 0, len(followKeys))
		faddrs := make([]string, 0, len(followKeys))
		for _, k := range followKeys {
			fvids = append(fvids, k.vid)
			faddrs = append(faddrs, k.addr)
		}
		var cnt int
		_ = r.pool.QueryRow(ctx, `
			SELECT COUNT(*)::int
			FROM trader_period_metrics
			WHERE period=$1 AND (venue_id, wallet_address) IN (SELECT unnest($2::uuid[]), unnest($3::text[]))`,
			period, fvids, faddrs).Scan(&cnt)
		if cnt < len(followKeys) {
			partial = true
		}
	}

	return &FollowOverview{
		FollowingCount:   len(followKeys),
		GroupsCount:      len(groupIDs),
		UniqueWallets:    len(set),
		OverlapWallets:   overlap,
		GroupOnlyWallets: groupOnly,
		AccountValue:     avPtr,
		UnrealizedPnL:    upnlPtr,
		Partial:          partial,
		AsOf:             time.Now(),
	}, nil
}
