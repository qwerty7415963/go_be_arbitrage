package tradergroup

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
	"github.com/qwerty7415963/go_be_arbitrage/internal/notify"
	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

// WalletCapProvider supplies the configured max wallets per user.
type WalletCapProvider interface {
	MaxWalletsPerUser(ctx context.Context) (int, error)
}

// Repository persists trader_groups + trader_group_members. Every read/write
// is owner-scoped: unknown group is 404, foreign group is 403 (BE-028).
type Repository struct {
	pool            *pgxpool.Pool
	capProvider     WalletCapProvider
	cursorSecret    []byte
	transferFetcher func(ctx context.Context, venueCode, addr string, days int) (*trader.TransfersPage, error)
	transferCache   sync.Map
	notifyRepo      *notify.Repository
}

func NewRepository(pool *pgxpool.Pool, capProviders ...WalletCapProvider) *Repository {
	r := &Repository{pool: pool}
	if len(capProviders) > 0 {
		r.capProvider = capProviders[0]
	}
	return r
}

func (r *Repository) WithAppSettings(p WalletCapProvider) *Repository {
	r.capProvider = p
	return r
}

func (r *Repository) maxWallets(ctx context.Context) int {
	if r.capProvider != nil {
		if v, err := r.capProvider.MaxWalletsPerUser(ctx); err == nil && v > 0 {
			return v
		}
	}
	return 50
}

func notFound() error {
	return domain.NewError(domain.ErrCodeNotFound, "group not found")
}

func forbidden() error {
	return domain.NewError(domain.ErrCodeGroupForbidden, "not your group")
}

// requireOwner resolves the group's owner: 404 unknown, 403 foreign.
func (r *Repository) requireOwner(ctx context.Context, groupID, userID uuid.UUID) error {
	var owner uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT user_id FROM trader_groups WHERE id = $1`, groupID).Scan(&owner)
	if err != nil {
		if err == pgx.ErrNoRows {
			return notFound()
		}
		return err
	}
	if owner != userID {
		return forbidden()
	}
	return nil
}

// Create inserts a group; duplicate name per user is GROUP-002 (409).
func (r *Repository) Create(ctx context.Context, userID uuid.UUID, name, description string) (*Group, error) {
	var g Group
	err := r.pool.QueryRow(ctx, `
		INSERT INTO trader_groups (user_id, name, description)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, name, description, is_favorite, notify_enabled, created_at, updated_at`,
		userID, name, description).Scan(&g.ID, &g.UserID, &g.Name, &g.Description,
		&g.IsFavorite, &g.NotifyEnabled, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.NewError(domain.ErrCodeGroupDuplicate, "group name already exists")
		}
		return nil, err
	}
	g.MemberCount = 0
	g.FollowedCount = 0
	return &g, nil
}

type walletKey struct {
	venueID uuid.UUID
	addr    string
}

func splitKeys(keys []walletKey) ([]uuid.UUID, []string) {
	vids := make([]uuid.UUID, 0, len(keys))
	addrs := make([]string, 0, len(keys))
	for _, k := range keys {
		vids = append(vids, k.venueID)
		addrs = append(addrs, k.addr)
	}
	return vids, addrs
}

func uniqueKeys(in []walletKey) []walletKey {
	seen := map[string]bool{}
	var out []walletKey
	for _, k := range in {
		key := k.venueID.String() + "|" + k.addr
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, k)
	}
	return out
}

// List returns the caller's groups with aggregates.
func (r *Repository) List(ctx context.Context, userID uuid.UUID, params ListGroupsParams) ([]*GroupListItem, int, error) {
	if params.Page < 1 {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page")
	}
	if params.PageSize < 1 || params.PageSize > 100 {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page_size")
	}
	if params.Period == "" {
		params.Period = trader.Period30D
	}
	switch params.Period {
	case trader.Period1D, trader.Period7D, trader.Period30D, trader.PeriodALL, "90D":
	default:
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period")
	}
	if params.Sort == "" {
		params.Sort = "name"
	}
	switch params.Sort {
	case "name", "account_value", "unrealized_pnl", "roi", "volume", "open_positions", "win_rate", "updated_at", "created_at":
	default:
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid sort")
	}
	if params.Dir != "asc" && params.Dir != "desc" {
		params.Dir = "asc"
	}
	if params.Status != "" {
		switch params.Status {
		case "empty", "ready", "partial", "stale", "error":
		default:
			return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid status")
		}
	}

	// base groups
	q := `SELECT id, name, description, is_favorite, notify_enabled, created_at, updated_at
		FROM trader_groups WHERE user_id = $1`
	args := []interface{}{userID}
	if params.Search != "" {
		q += ` AND (name ILIKE $2 OR description ILIKE $2)`
		args = append(args, "%"+params.Search+"%")
	}
	if params.Favorite != nil {
		idx := len(args) + 1
		q += fmt.Sprintf(` AND is_favorite = $%d`, idx)
		args = append(args, *params.Favorite)
	}
	q += ` ORDER BY name ASC`
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	type baseGroup struct {
		id            uuid.UUID
		name          string
		description   string
		isFavorite    bool
		notifyEnabled bool
		createdAt     time.Time
		updatedAt     time.Time
	}
	var groups []baseGroup
	for rows.Next() {
		var g baseGroup
		if err := rows.Scan(&g.id, &g.name, &g.description, &g.isFavorite, &g.notifyEnabled, &g.createdAt, &g.updatedAt); err != nil {
			rows.Close()
			return nil, 0, err
		}
		groups = append(groups, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if len(groups) == 0 {
		return []*GroupListItem{}, 0, nil
	}

	groupIDs := make([]uuid.UUID, 0, len(groups))
	for _, g := range groups {
		groupIDs = append(groupIDs, g.id)
	}

	// batch followed counts
	groupFollowedCount := map[uuid.UUID]int{}
	if len(groupIDs) > 0 {
		frows, err := r.pool.Query(ctx, `
			SELECT m.group_id, COUNT(DISTINCT f.wallet_address)::int
			FROM trader_group_members m
			LEFT JOIN trader_follows f
			  ON f.venue_id = m.venue_id AND f.wallet_address = m.wallet_address AND f.user_id = $2
			WHERE m.group_id = ANY($1)
			GROUP BY m.group_id`, groupIDs, userID)
		if err == nil {
			for frows.Next() {
				var gid uuid.UUID
				var n int
				if err := frows.Scan(&gid, &n); err == nil {
					groupFollowedCount[gid] = n
				}
			}
			frows.Close()
		}
	}

	// batch membership
	mrows, err := r.pool.Query(ctx, `
		SELECT group_id, venue_id, wallet_address
		FROM trader_group_members
		WHERE group_id = ANY($1)`, groupIDs)
	if err != nil {
		return nil, 0, err
	}
	groupWallets := map[uuid.UUID][]walletKey{}
	allKeys := []walletKey{}
	for mrows.Next() {
		var gid uuid.UUID
		var vid uuid.UUID
		var addr string
		if err := mrows.Scan(&gid, &vid, &addr); err != nil {
			mrows.Close()
			return nil, 0, err
		}
		k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
		groupWallets[gid] = append(groupWallets[gid], k)
		allKeys = append(allKeys, k)
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, 0, err
	}
	allKeys = uniqueKeys(allKeys)

	// batch fetch wallet data
	summaryAV := map[walletKey]*float64{}
	positionsUPNL := map[walletKey]*float64{}
	positionsCount := map[walletKey]int{}
	syncStatus := map[walletKey]string{}
	syncLast := map[walletKey]*time.Time{}
	periodMetrics := map[walletKey]*trader.PeriodMetrics{}
	equityDays := map[walletKey][]struct {
		date  time.Time
		start *float64
		end   *float64
	}{}
	latestEquityEnd := map[walletKey]*float64{}

	if len(allKeys) > 0 {
		vids, addrs := splitKeys(allKeys)

		// summary
		srows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, account_value
			FROM trader_position_summary
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if err == nil {
			for srows.Next() {
				var vid uuid.UUID
				var addr string
				var av *float64
				if err := srows.Scan(&vid, &addr, &av); err != nil {
					continue
				}
				summaryAV[walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}] = av
			}
			srows.Close()
		}

		// positions aggregate
		prows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, SUM(unrealized_pnl), COUNT(*)::int
			FROM trader_positions
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))
			GROUP BY venue_id, wallet_address`, vids, addrs)
		if err == nil {
			for prows.Next() {
				var vid uuid.UUID
				var addr string
				var upnl *float64
				var cnt int
				if err := prows.Scan(&vid, &addr, &upnl, &cnt); err != nil {
					continue
				}
				k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
				positionsUPNL[k] = upnl
				positionsCount[k] = cnt
			}
			prows.Close()
		}

		// sync state
		ssrows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, sync_status, last_positions_sync_at
			FROM trader_sync_state
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if err == nil {
			for ssrows.Next() {
				var vid uuid.UUID
				var addr string
				var status string
				var last *time.Time
				if err := ssrows.Scan(&vid, &addr, &status, &last); err != nil {
					continue
				}
				k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
				syncStatus[k] = status
				syncLast[k] = last
			}
			ssrows.Close()
		}

		// period metrics
		pmrows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, period, as_of, pnl, realized_pnl, roi, win_rate, trade_count, volume, gross_profit, gross_loss,
				profit_factor, avg_trade_pnl, long_count, long_wins, short_count, short_wins,
				max_drawdown_pct, avg_holding_time_sec, last_trade_at, data_status, is_partial, calculation_version
			FROM trader_period_metrics
			WHERE period = $1 AND (venue_id, wallet_address) IN (SELECT unnest($2::uuid[]), unnest($3::text[]))`, params.Period, vids, addrs)
		if err == nil {
			for pmrows.Next() {
				var prow periodRow
				var vid uuid.UUID
				var addr string
				// scan into temp variables with vid/addr first
				var period *string
				var asOf *time.Time
				var pnl, realizedPnL, roi, winRate, volume, grossProfit, grossLoss, profitFactor, avgTradePnL, maxDD, avgHold *float64
				var tradeCount, longCount, longWins, shortCount, shortWins *int64
				var lastTradeAt *time.Time
				var dataStatus *string
				var isPartial *bool
				var calcVer *int
				if err := pmrows.Scan(&vid, &addr, &period, &asOf, &pnl, &realizedPnL, &roi, &winRate, &tradeCount, &volume, &grossProfit, &grossLoss,
					&profitFactor, &avgTradePnL, &longCount, &longWins, &shortCount, &shortWins,
					&maxDD, &avgHold, &lastTradeAt, &dataStatus, &isPartial, &calcVer); err != nil {
					continue
				}
				prow.Period = period
				prow.AsOf = asOf
				prow.PnL = pnl
				prow.RealizedPnL = realizedPnL
				prow.ROI = roi
				prow.WinRate = winRate
				prow.TradeCount = tradeCount
				prow.Volume = volume
				prow.GrossProfit = grossProfit
				prow.GrossLoss = grossLoss
				prow.ProfitFactor = profitFactor
				prow.AvgTradePnL = avgTradePnL
				prow.LongCount = longCount
				prow.LongWins = longWins
				prow.ShortCount = shortCount
				prow.ShortWins = shortWins
				prow.MaxDrawdownPct = maxDD
				prow.AvgHoldingTimeSec = avgHold
				prow.LastTradeAt = lastTradeAt
				prow.DataStatus = dataStatus
				prow.IsPartial = isPartial
				prow.CalculationVersion = calcVer
				pm := prow.toMetrics(vid, "", strings.ToLower(strings.TrimSpace(addr)), nil)
				periodMetrics[walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}] = pm
			}
			pmrows.Close()
		}

		// equity days
		erows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, stat_date, start_equity, end_equity
			FROM trader_equity_daily
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if err == nil {
			for erows.Next() {
				var vid uuid.UUID
				var addr string
				var d time.Time
				var s, e *float64
				if err := erows.Scan(&vid, &addr, &d, &s, &e); err != nil {
					continue
				}
				k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
				equityDays[k] = append(equityDays[k], struct {
					date  time.Time
					start *float64
					end   *float64
				}{date: d, start: s, end: e})
				// latest end equity fallback
				if e != nil {
					if cur, ok := latestEquityEnd[k]; !ok || cur == nil {
						v := *e
						latestEquityEnd[k] = &v
					}
				}
			}
			erows.Close()
		}
		// fix latestEquityEnd to max date
		latestEquityEnd = map[walletKey]*float64{}
		for k, days := range equityDays {
			var latest time.Time
			var val *float64
			for _, d := range days {
				if d.end != nil && (val == nil || d.date.After(latest)) {
					v := *d.end
					val = &v
					latest = d.date
				}
			}
			latestEquityEnd[k] = val
		}
	}

	// period from date
	now := time.Now().UTC()
	var fromDate time.Time
	switch params.Period {
	case trader.Period1D:
		fromDate = now.AddDate(0, 0, -1)
	case trader.Period7D:
		fromDate = now.AddDate(0, 0, -7)
	case trader.Period30D:
		fromDate = now.AddDate(0, 0, -30)
	case "90D":
		fromDate = now.AddDate(0, 0, -90)
	case trader.PeriodALL:
		fromDate = now.AddDate(-2, 0, 0)
	}
	fromDay := fromDate.Truncate(24 * time.Hour)

	items := make([]*GroupListItem, 0, len(groups))
	for _, g := range groups {
		wallets := groupWallets[g.id]
		memberCount := len(wallets)

		item := &GroupListItem{
			ID:            g.id,
			Name:          g.name,
			Description:   g.description,
			IsFavorite:    g.isFavorite,
			NotifyEnabled: g.notifyEnabled,
			MemberCount:   memberCount,
			FollowedCount: groupFollowedCount[g.id],
			UpdatedAt:     g.updatedAt,
			OpenPositions: 0,
		}

		if memberCount == 0 {
			item.DataStatus = "empty"
			item.Partial = false
			item.MissingWallets = 0
			items = append(items, item)
			continue
		}

		var (
			accountValueSum float64
			hasAccountValue bool
			unrealizedSum   float64
			hasUnrealized   bool
			openPosSum      int
			missing         int
			membersWithData int
			allReady        = true
			newest          time.Time
			hasTS           bool
		)
		var (
			volumeSum float64
			hasVolume bool
			winSum    int64
			totalCnt  int64
			hasWinDen bool
		)

		// TWR aggregation per day
		dayAgg := map[time.Time]struct {
			start float64
			end   float64
			has   bool
		}{}

		for _, wl := range wallets {
			k := wl
			// account value
			av := summaryAV[k]
			if av == nil {
				av = latestEquityEnd[k]
			}
			hasWalletData := false
			if av != nil {
				accountValueSum += *av
				hasAccountValue = true
				hasWalletData = true
			}

			// unrealized pnl
			upnl := positionsUPNL[k]
			posCount := positionsCount[k]
			if upnl != nil {
				unrealizedSum += *upnl
				hasUnrealized = true
				hasWalletData = true
			}
			openPosSum += posCount
			if posCount > 0 {
				hasWalletData = true
			}

			// sync status
			ss := syncStatus[k]
			last := syncLast[k]
			ready := ss == "ready"
			if last != nil {
				if time.Since(*last) > 15*time.Minute {
					ready = false
				}
			} else if ss == "ready" {
				ready = false
			}
			if ss != "ready" {
				ready = false
			}
			if !ready {
				allReady = false
			}
			staleWallet := false
			if hasWalletData {
				if last != nil {
					if time.Since(*last) > 24*time.Hour {
						staleWallet = true
					}
					if !hasTS || last.After(newest) {
						newest = *last
						hasTS = true
					}
				} else {
					staleWallet = true
				}
			}
			_ = staleWallet

			if !hasWalletData {
				missing++
			} else {
				membersWithData++
			}

			// period metrics
			pm := periodMetrics[k]
			if pm != nil {
				if pm.Volume != nil {
					volumeSum += *pm.Volume
					hasVolume = true
				}
				if pm.LongCount != nil {
					winSum += int64Or(pm.LongWins) + int64Or(pm.ShortWins)
					totalCnt += int64Or(pm.LongCount) + int64Or(pm.ShortCount)
					hasWinDen = true
				}
			}

			// TWR equity days
			for _, d := range equityDays[k] {
				if d.start != nil && d.end != nil {
					entry := dayAgg[d.date]
					entry.start += *d.start
					entry.end += *d.end
					entry.has = true
					dayAgg[d.date] = entry
				}
			}
		}

		// anyStale: newest member data >24h
		anyStale := false
		if membersWithData > 0 {
			if !hasTS || time.Since(newest) > 24*time.Hour {
				anyStale = true
			}
		}

		// build points
		var points []TWREquityPoint
		for d, v := range dayAgg {
			if d.Before(fromDay) {
				continue
			}
			if !v.has {
				continue
			}
			pt := TWREquityPoint{Date: d, Start: v.start, End: v.end, Valid: v.start > 0}
			points = append(points, pt)
		}
		roi := TWRChain(points)

		if hasAccountValue {
			v := accountValueSum
			item.AccountValue = &v
		}
		if hasUnrealized {
			v := unrealizedSum
			item.UnrealizedPnL = &v
		}
		item.OpenPositions = openPosSum
		item.MissingWallets = missing
		item.Partial = missing > 0 || !allReady || anyStale
		if hasVolume {
			v := volumeSum
			item.Volume = &v
		}
		if hasWinDen && totalCnt > 0 {
			v := float64(winSum) / float64(totalCnt)
			item.WinRate = &v
		}
		item.ROI = roi
		item.DataStatus = DeriveGroupStatus(memberCount, membersWithData, allReady, anyStale)

		if params.Status != "" && item.DataStatus != params.Status {
			continue
		}
		items = append(items, item)
	}

	SortGroupListItems(items, params.Sort, params.Dir)

	total := len(items)
	start := (params.Page - 1) * params.PageSize
	if start > total {
		start = total
	}
	end := start + params.PageSize
	if end > total {
		end = total
	}
	pageItems := items[start:end]
	return pageItems, total, nil
}

// Get returns one owned group with its member count.
func (r *Repository) Get(ctx context.Context, groupID, userID uuid.UUID) (*Group, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}
	var g Group
	err := r.pool.QueryRow(ctx, `
		SELECT g.id, g.user_id, g.name, g.description, g.is_favorite, g.notify_enabled,
			COUNT(m.wallet_address)::int, g.created_at, g.updated_at
		FROM trader_groups g LEFT JOIN trader_group_members m ON m.group_id = g.id
		WHERE g.id = $1 GROUP BY g.id`, groupID,
	).Scan(&g.ID, &g.UserID, &g.Name, &g.Description, &g.IsFavorite, &g.NotifyEnabled,
		&g.MemberCount, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = r.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT f.wallet_address)::int FROM trader_group_members m LEFT JOIN trader_follows f ON f.venue_id=m.venue_id AND f.wallet_address=m.wallet_address AND f.user_id=$2 WHERE m.group_id=$1`, groupID, userID).Scan(&g.FollowedCount)
	return &g, nil
}

// Update renames/edits an owned group; rename collision is GROUP-002.
func (r *Repository) Update(ctx context.Context, groupID, userID uuid.UUID, name, description *string, isFavorite, notifyEnabled *bool) (*Group, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}
	var g Group
	err := r.pool.QueryRow(ctx, `
		UPDATE trader_groups SET
			name = COALESCE($3, name),
			description = COALESCE($4, description),
			is_favorite = COALESCE($5, is_favorite),
			notify_enabled = COALESCE($6, notify_enabled),
			updated_at = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, name, description, is_favorite, notify_enabled, created_at, updated_at`,
		groupID, userID, name, description, isFavorite, notifyEnabled).Scan(&g.ID, &g.UserID, &g.Name, &g.Description,
		&g.IsFavorite, &g.NotifyEnabled, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.NewError(domain.ErrCodeGroupDuplicate, "group name already exists")
		}
		return nil, err
	}
	_ = r.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM trader_group_members WHERE group_id = $1`, groupID).Scan(&g.MemberCount)
	_ = r.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT f.wallet_address)::int FROM trader_group_members m LEFT JOIN trader_follows f ON f.venue_id=m.venue_id AND f.wallet_address=m.wallet_address AND f.user_id=$2 WHERE m.group_id=$1`, groupID, userID).Scan(&g.FollowedCount)
	return &g, nil
}

// Delete removes an owned group; memberships cascade, registry rows stay (BE-030).
func (r *Repository) Delete(ctx context.Context, groupID, userID uuid.UUID) error {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM trader_groups WHERE id = $1`, groupID)
	return err
}

// AddMembers inserts memberships idempotently and writes membership events.
func (r *Repository) AddMembers(ctx context.Context, groupID, userID uuid.UUID, items []MemberInput) (int64, int64, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return 0, 0, err
	}

	// normalize addresses lowercase
	normItems := make([]MemberInput, 0, len(items))
	for _, it := range items {
		it.Venue = strings.ToLower(strings.TrimSpace(it.Venue))
		it.WalletAddress = strings.ToLower(strings.TrimSpace(it.WalletAddress))
		it.Alias = strings.TrimSpace(it.Alias)
		it.Note = strings.TrimSpace(it.Note)
		normItems = append(normItems, it)
	}
	items = normItems

	// Cap check (contract v1.5): distinct watched wallets per user.
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT wallet_address FROM trader_group_members
		WHERE group_id IN (SELECT id FROM trader_groups WHERE user_id = $1)`, userID)
	if err != nil {
		return 0, 0, err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			rows.Close()
			return 0, 0, err
		}
		existing[strings.ToLower(strings.TrimSpace(addr))] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	current := len(existing)

	newCount := 0
	newSet := map[string]bool{}
	for _, it := range items {
		addr := it.WalletAddress
		if existing[addr] {
			continue
		}
		if newSet[addr] {
			continue
		}
		newSet[addr] = true
		newCount++
	}

	maxWallets := r.maxWallets(ctx)
	if current+newCount > maxWallets {
		return 0, 0, domain.NewError(domain.ErrCodeTelegramWalletCap, fmt.Sprintf("wallet cap exceeded: %d/%d", current, maxWallets))
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	var added, skipped int64
	for _, it := range items {
		var venueID uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT id FROM venues WHERE code = $1`, it.Venue).Scan(&venueID); err != nil {
			if err == pgx.ErrNoRows {
				return 0, 0, domain.NewError(domain.ErrCodeValidation, "unknown venue: "+it.Venue)
			}
			return 0, 0, err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2)`,
			venueID, it.WalletAddress).Scan(&exists); err != nil {
			return 0, 0, err
		}
		if !exists {
			return 0, 0, domain.NewError(domain.ErrCodeNotFound, "unknown wallet: "+it.WalletAddress)
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO trader_group_members (group_id, venue_id, wallet_address, alias, note)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (group_id, venue_id, wallet_address) DO NOTHING`,
			groupID, venueID, it.WalletAddress, it.Alias, it.Note)
		if err != nil {
			return 0, 0, err
		}
		if tag.RowsAffected() == 1 {
			added++
			_, _ = tx.Exec(ctx, `
				INSERT INTO group_membership_events (group_id, venue_id, wallet_address, event_type, actor_user_id)
				VALUES ($1, $2, $3, 'added', $4)`, groupID, venueID, it.WalletAddress, userID)
		} else {
			skipped++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return added, skipped, nil
}

// RemoveMembers deletes memberships and writes removal events.
func (r *Repository) RemoveMembers(ctx context.Context, groupID, userID uuid.UUID, venueID uuid.UUID, addrs []string) (int64, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return 0, err
	}
	if len(addrs) == 0 {
		return 0, nil
	}
	norm := make([]string, 0, len(addrs))
	for _, a := range addrs {
		norm = append(norm, strings.ToLower(strings.TrimSpace(a)))
	}
	addrs = norm

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		DELETE FROM trader_group_members
		WHERE group_id = $1 AND venue_id = $2 AND wallet_address = ANY($3)
		RETURNING venue_id, wallet_address`, groupID, venueID, addrs)
	if err != nil {
		return 0, err
	}
	type removed struct {
		venue uuid.UUID
		addr  string
	}
	var removedRows []removed
	for rows.Next() {
		var rr removed
		if err := rows.Scan(&rr.venue, &rr.addr); err != nil {
			rows.Close()
			return 0, err
		}
		removedRows = append(removedRows, rr)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, rr := range removedRows {
		_, _ = tx.Exec(ctx, `
			INSERT INTO group_membership_events (group_id, venue_id, wallet_address, event_type, actor_user_id)
			VALUES ($1, $2, $3, 'removed', $4)`, groupID, rr.venue, rr.addr, userID)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int64(len(removedRows)), nil
}

// UpdateMembers sets alias/note on existing memberships.
func (r *Repository) UpdateMembers(ctx context.Context, groupID, userID uuid.UUID, items []ValidatedMemberUpdate) (int64, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return 0, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var updated int64
	for _, it := range items {
		it.WalletAddress = strings.ToLower(strings.TrimSpace(it.WalletAddress))
		it.Venue = strings.ToLower(strings.TrimSpace(it.Venue))
		var venueID uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT id FROM venues WHERE code = $1`, it.Venue).Scan(&venueID); err != nil {
			if err == pgx.ErrNoRows {
				return 0, domain.NewError(domain.ErrCodeValidation, "unknown venue: "+it.Venue)
			}
			return 0, err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM trader_registry WHERE venue_id = $1 AND wallet_address = $2)`,
			venueID, it.WalletAddress).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			return 0, domain.NewError(domain.ErrCodeNotFound, "unknown wallet: "+it.WalletAddress)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE trader_group_members
			SET alias = CASE WHEN $4 THEN NULLIF($5, '') ELSE alias END,
			    note = CASE WHEN $6 THEN NULLIF($7, '') ELSE note END,
			    updated_at = NOW()
			WHERE group_id = $1 AND venue_id = $2 AND wallet_address = $3
			  AND (CASE WHEN $4 THEN NULLIF($5, '') ELSE alias END IS DISTINCT FROM alias
			    OR CASE WHEN $6 THEN NULLIF($7, '') ELSE note END IS DISTINCT FROM note)`,
			groupID, venueID, it.WalletAddress,
			it.SetAlias, it.Alias, it.SetNote, it.Note)
		if err != nil {
			return 0, err
		}
		updated += tag.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return updated, nil
}

// ListMembers returns owned-group memberships with aggregates.
func (r *Repository) ListMembers(ctx context.Context, groupID, userID uuid.UUID, params ListMembersParams) ([]*MemberRow, int, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, 0, err
	}
	if params.Page < 1 {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page")
	}
	if params.PageSize < 1 || params.PageSize > 100 {
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid page_size")
	}
	if params.Period == "" {
		params.Period = trader.Period30D
	}
	switch params.Period {
	case trader.Period1D, trader.Period7D, trader.Period30D, trader.PeriodALL, "90D":
	default:
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid period")
	}
	if params.Sort == "" {
		params.Sort = "wallet_address"
	}
	switch params.Sort {
	case "wallet_address", "account_value", "unrealized_pnl", "roi", "volume", "open_positions", "last_trade_at", "updated_at":
	default:
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid sort")
	}
	if params.Dir != "asc" && params.Dir != "desc" {
		params.Dir = "asc"
	}
	if params.Filter == "" {
		params.Filter = "all"
	}
	switch params.Filter {
	case "all", "missing_data", "stale", "in_other_groups":
	default:
		return nil, 0, domain.NewError(domain.ErrCodeInvalidFilter, "invalid filter")
	}

	// fetch members
	mrows, err := r.pool.Query(ctx, `
		SELECT m.venue_id, v.code, m.wallet_address, COALESCE(f.alias, r.display_name), m.alias, m.note, m.updated_at,
		       CASE WHEN f.user_id IS NOT NULL THEN TRUE ELSE FALSE END
		FROM trader_group_members m
		JOIN venues v ON v.id = m.venue_id
		LEFT JOIN trader_registry r ON r.venue_id = m.venue_id AND r.wallet_address = m.wallet_address
		LEFT JOIN trader_follows f ON f.venue_id = m.venue_id AND f.wallet_address = m.wallet_address AND f.user_id = $2
		WHERE m.group_id = $1`, groupID, userID)
	if err != nil {
		return nil, 0, err
	}
	type memberBase struct {
		venueID    uuid.UUID
		venue      string
		addr       string
		display    *string
		alias      *string
		note       *string
		updated    time.Time
		isFollowed bool
	}
	var bases []memberBase
	for mrows.Next() {
		var b memberBase
		if err := mrows.Scan(&b.venueID, &b.venue, &b.addr, &b.display, &b.alias, &b.note, &b.updated, &b.isFollowed); err != nil {
			mrows.Close()
			return nil, 0, err
		}
		bases = append(bases, b)
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, 0, err
	}

	// in_groups batch: memberships across all user groups
	groupsByWallet := map[string][]GroupRef{}
	grows, err := r.pool.Query(ctx, `
		SELECT m.venue_id, m.wallet_address, g.id, g.name
		FROM trader_group_members m
		JOIN trader_groups g ON g.id = m.group_id
		WHERE g.user_id = $1`, userID)
	if err != nil {
		return nil, 0, err
	}
	for grows.Next() {
		var venueID uuid.UUID
		var addr string
		var gid uuid.UUID
		var gname string
		if err := grows.Scan(&venueID, &addr, &gid, &gname); err != nil {
			grows.Close()
			return nil, 0, err
		}
		key := venueID.String() + "|" + strings.ToLower(strings.TrimSpace(addr))
		groupsByWallet[key] = append(groupsByWallet[key], GroupRef{ID: gid, Name: gname})
	}
	grows.Close()
	if err := grows.Err(); err != nil {
		return nil, 0, err
	}

	rowsOut := make([]*MemberRow, 0, len(bases))
	for _, b := range bases {
		mr := &MemberRow{
			VenueID:       b.venueID,
			Venue:         b.venue,
			WalletAddress: strings.ToLower(strings.TrimSpace(b.addr)),
			DisplayName:   b.display,
			Alias:         b.alias,
			Note:          b.note,
			UpdatedAt:     b.updated,
			IsFollowed:    b.isFollowed,
			InGroups:      groupsByWallet[b.venueID.String()+"|"+strings.ToLower(strings.TrimSpace(b.addr))],
		}
		if mr.InGroups == nil {
			mr.InGroups = []GroupRef{}
		}

		// account value
		var av *float64
		_ = r.pool.QueryRow(ctx, `SELECT account_value FROM trader_position_summary WHERE venue_id=$1 AND wallet_address=$2`, b.venueID, b.addr).Scan(&av)
		if av == nil {
			var eq *float64
			_ = r.pool.QueryRow(ctx, `SELECT end_equity FROM trader_equity_daily WHERE venue_id=$1 AND wallet_address=$2 ORDER BY stat_date DESC LIMIT 1`, b.venueID, b.addr).Scan(&eq)
			av = eq
		}
		mr.AccountValue = av

		// unrealized pnl
		var upnl *float64
		_ = r.pool.QueryRow(ctx, `SELECT SUM(unrealized_pnl) FROM trader_positions WHERE venue_id=$1 AND wallet_address=$2`, b.venueID, b.addr).Scan(&upnl)
		mr.UnrealizedPnL = upnl

		// open positions
		var posCount int
		_ = r.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM trader_positions WHERE venue_id=$1 AND wallet_address=$2`, b.venueID, b.addr).Scan(&posCount)
		mr.OpenPositions = posCount

		// period metrics
		var prow periodRow
		err = r.pool.QueryRow(ctx, `
			SELECT period, as_of, pnl, realized_pnl, roi, win_rate, trade_count, volume, gross_profit, gross_loss,
				profit_factor, avg_trade_pnl, long_count, long_wins, short_count, short_wins,
				max_drawdown_pct, avg_holding_time_sec, last_trade_at, data_status, is_partial, calculation_version
			FROM trader_period_metrics WHERE venue_id=$1 AND wallet_address=$2 AND period=$3`,
			b.venueID, b.addr, params.Period).Scan(&prow.Period, &prow.AsOf, &prow.PnL, &prow.RealizedPnL, &prow.ROI,
			&prow.WinRate, &prow.TradeCount, &prow.Volume, &prow.GrossProfit, &prow.GrossLoss,
			&prow.ProfitFactor, &prow.AvgTradePnL, &prow.LongCount, &prow.LongWins, &prow.ShortCount,
			&prow.ShortWins, &prow.MaxDrawdownPct, &prow.AvgHoldingTimeSec, &prow.LastTradeAt,
			&prow.DataStatus, &prow.IsPartial, &prow.CalculationVersion)
		if err == nil && prow.Period != nil {
			pm := prow.toMetrics(b.venueID, b.venue, b.addr, b.display)
			if pm.Volume != nil {
				mr.Volume = pm.Volume
			}
			if pm.ROI != nil {
				mr.ROI = pm.ROI
			}
			mr.LastTradeAt = pm.LastTradeAt
		}

		// data status
		var syncStatus string
		var lastPosSync *time.Time
		_ = r.pool.QueryRow(ctx, `SELECT sync_status, last_positions_sync_at FROM trader_sync_state WHERE venue_id=$1 AND wallet_address=$2`, b.venueID, b.addr).Scan(&syncStatus, &lastPosSync)
		hasData := av != nil || upnl != nil || posCount > 0
		if !hasData {
			mr.DataStatus = "error"
		} else if syncStatus == "ready" {
			if lastPosSync != nil && time.Since(*lastPosSync) <= 15*time.Minute {
				mr.DataStatus = "ready"
			} else if lastPosSync != nil && time.Since(*lastPosSync) > 24*time.Hour {
				mr.DataStatus = "stale"
			} else {
				mr.DataStatus = "ready"
			}
		} else if syncStatus == "error" {
			mr.DataStatus = "error"
		} else if syncStatus == "syncing" || syncStatus == "pending" {
			mr.DataStatus = "syncing"
		} else {
			mr.DataStatus = "stale"
		}
		if lastPosSync == nil && hasData && syncStatus != "ready" {
			mr.DataStatus = "stale"
		}

		// search filter
		if params.Search != "" {
			s := strings.ToLower(params.Search)
			match := strings.Contains(strings.ToLower(b.addr), s)
			if b.display != nil && strings.Contains(strings.ToLower(*b.display), s) {
				match = true
			}
			if b.alias != nil && strings.Contains(strings.ToLower(*b.alias), s) {
				match = true
			}
			if !match {
				continue
			}
		}

		// filter
		switch params.Filter {
		case "missing_data":
			if hasData {
				continue
			}
		case "stale":
			if mr.DataStatus != "stale" {
				continue
			}
		case "in_other_groups":
			if len(mr.InGroups) <= 1 {
				continue
			}
		}

		rowsOut = append(rowsOut, mr)
	}

	// sort
	sort.SliceStable(rowsOut, func(i, j int) bool {
		less := memberLess(rowsOut[i], rowsOut[j], params.Sort)
		if params.Dir == "desc" {
			less = memberLess(rowsOut[j], rowsOut[i], params.Sort)
		}
		if memberEqual(rowsOut[i], rowsOut[j], params.Sort) {
			return rowsOut[i].WalletAddress < rowsOut[j].WalletAddress
		}
		return less
	})

	total := len(rowsOut)
	start := (params.Page - 1) * params.PageSize
	if start > total {
		start = total
	}
	end := start + params.PageSize
	if end > total {
		end = total
	}
	return rowsOut[start:end], total, nil
}

func memberLess(a, b *MemberRow, field string) bool {
	switch field {
	case "wallet_address":
		return a.WalletAddress < b.WalletAddress
	case "account_value":
		return floatPtrLess(a.AccountValue, b.AccountValue)
	case "unrealized_pnl":
		return floatPtrLess(a.UnrealizedPnL, b.UnrealizedPnL)
	case "roi":
		return floatPtrLess(a.ROI, b.ROI)
	case "volume":
		return floatPtrLess(a.Volume, b.Volume)
	case "open_positions":
		return a.OpenPositions < b.OpenPositions
	case "last_trade_at":
		return timePtrLess(a.LastTradeAt, b.LastTradeAt)
	case "updated_at":
		return a.UpdatedAt.Before(b.UpdatedAt)
	default:
		return a.WalletAddress < b.WalletAddress
	}
}

func memberEqual(a, b *MemberRow, field string) bool {
	switch field {
	case "wallet_address":
		return a.WalletAddress == b.WalletAddress
	case "account_value":
		return floatPtrEqual(a.AccountValue, b.AccountValue)
	case "unrealized_pnl":
		return floatPtrEqual(a.UnrealizedPnL, b.UnrealizedPnL)
	case "roi":
		return floatPtrEqual(a.ROI, b.ROI)
	case "volume":
		return floatPtrEqual(a.Volume, b.Volume)
	case "open_positions":
		return a.OpenPositions == b.OpenPositions
	case "last_trade_at":
		return timePtrEqual(a.LastTradeAt, b.LastTradeAt)
	case "updated_at":
		return a.UpdatedAt.Equal(b.UpdatedAt)
	default:
		return false
	}
}

func timePtrLess(a, b *time.Time) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	return a.Before(*b)
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// Duplicate copies a group and its members in a transaction.
func (r *Repository) Duplicate(ctx context.Context, groupID, userID uuid.UUID, name *string) (*Group, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}
	var origName, origDesc string
	if err := r.pool.QueryRow(ctx, `SELECT name, description FROM trader_groups WHERE id = $1`, groupID).Scan(&origName, &origDesc); err != nil {
		return nil, err
	}
	desired := origName
	if name != nil && strings.TrimSpace(*name) != "" {
		desired = strings.TrimSpace(*name)
	} else {
		desired = origName + " (copy)"
	}

	tryInsert := func(tx pgx.Tx, n string) (*Group, error) {
		var g Group
		err := tx.QueryRow(ctx, `
			INSERT INTO trader_groups (user_id, name, description, is_favorite, notify_enabled)
			VALUES ($1, $2, $3, FALSE, FALSE)
			RETURNING id, user_id, name, description, is_favorite, notify_enabled, created_at, updated_at`,
			userID, n, origDesc).Scan(&g.ID, &g.UserID, &g.Name, &g.Description,
			&g.IsFavorite, &g.NotifyEnabled, &g.CreatedAt, &g.UpdatedAt)
		if err != nil {
			return nil, err
		}
		g.MemberCount = 0
		g.FollowedCount = 0
		return &g, nil
	}

	// first attempt
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	g, err := tryInsert(tx, desired)
	if err != nil && isUniqueViolation(err) {
		tx.Rollback(ctx)
		tx, err = r.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		g, err = tryInsert(tx, desired+" (copy)")
		if err != nil {
			tx.Rollback(ctx)
			if isUniqueViolation(err) {
				return nil, domain.NewError(domain.ErrCodeGroupDuplicate, "group name already exists")
			}
			return nil, err
		}
	} else if err != nil {
		tx.Rollback(ctx)
		return nil, err
	}

	// copy members
	rows, err := tx.Query(ctx, `SELECT venue_id, wallet_address, alias, note FROM trader_group_members WHERE group_id = $1`, groupID)
	if err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	type m struct {
		venue uuid.UUID
		addr  string
		alias *string
		note  *string
	}
	var members []m
	for rows.Next() {
		var mm m
		if err := rows.Scan(&mm.venue, &mm.addr, &mm.alias, &mm.note); err != nil {
			rows.Close()
			tx.Rollback(ctx)
			return nil, err
		}
		members = append(members, mm)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	for _, mm := range members {
		_, err := tx.Exec(ctx, `
			INSERT INTO trader_group_members (group_id, venue_id, wallet_address, alias, note)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (group_id, venue_id, wallet_address) DO NOTHING`,
			g.ID, mm.venue, mm.addr, mm.alias, mm.note)
		if err != nil {
			tx.Rollback(ctx)
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	_ = r.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM trader_group_members WHERE group_id = $1`, g.ID).Scan(&g.MemberCount)
	return g, nil
}

// ToggleFavorite updates is_favorite.
func (r *Repository) ToggleFavorite(ctx context.Context, groupID, userID uuid.UUID, isFavorite bool) (*Group, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}
	var g Group
	err := r.pool.QueryRow(ctx, `
		UPDATE trader_groups SET is_favorite = $3, updated_at = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, name, description, is_favorite, notify_enabled, created_at, updated_at`,
		groupID, userID, isFavorite).Scan(&g.ID, &g.UserID, &g.Name, &g.Description,
		&g.IsFavorite, &g.NotifyEnabled, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = r.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM trader_group_members WHERE group_id = $1`, groupID).Scan(&g.MemberCount)
	return &g, nil
}

// Overview aggregates across all user groups.
func (r *Repository) Overview(ctx context.Context, userID uuid.UUID, period string) (*OverviewStats, error) {
	if period == "" {
		period = trader.Period30D
	}
	// groups
	grows, err := r.pool.Query(ctx, `SELECT id FROM trader_groups WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	var groupIDs []uuid.UUID
	for grows.Next() {
		var id uuid.UUID
		if err := grows.Scan(&id); err != nil {
			grows.Close()
			return nil, err
		}
		groupIDs = append(groupIDs, id)
	}
	grows.Close()
	if err := grows.Err(); err != nil {
		return nil, err
	}

	// memberships across groups
	mrows, err := r.pool.Query(ctx, `
		SELECT m.group_id, m.venue_id, m.wallet_address
		FROM trader_group_members m
		JOIN trader_groups g ON g.id = m.group_id
		WHERE g.user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	groupWallets := map[uuid.UUID][]walletKey{}
	allKeys := []walletKey{}
	for mrows.Next() {
		var gid uuid.UUID
		var vid uuid.UUID
		var addr string
		if err := mrows.Scan(&gid, &vid, &addr); err != nil {
			mrows.Close()
			return nil, err
		}
		k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
		groupWallets[gid] = append(groupWallets[gid], k)
		allKeys = append(allKeys, k)
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}
	allKeys = uniqueKeys(allKeys)

	// batch wallet data
	summaryAV := map[walletKey]*float64{}
	positionsUPNL := map[walletKey]*float64{}
	positionsCount := map[walletKey]int{}
	syncLast := map[walletKey]*time.Time{}
	syncStatus := map[walletKey]string{}
	latestEquityEnd := map[walletKey]*float64{}

	if len(allKeys) > 0 {
		vids, addrs := splitKeys(allKeys)
		srows, _ := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, account_value
			FROM trader_position_summary
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if srows != nil {
			for srows.Next() {
				var vid uuid.UUID
				var addr string
				var av *float64
				if err := srows.Scan(&vid, &addr, &av); err == nil {
					summaryAV[walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}] = av
				}
			}
			srows.Close()
		}
		prows, _ := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, SUM(unrealized_pnl), COUNT(*)::int
			FROM trader_positions
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))
			GROUP BY venue_id, wallet_address`, vids, addrs)
		if prows != nil {
			for prows.Next() {
				var vid uuid.UUID
				var addr string
				var upnl *float64
				var cnt int
				if err := prows.Scan(&vid, &addr, &upnl, &cnt); err == nil {
					k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
					positionsUPNL[k] = upnl
					positionsCount[k] = cnt
				}
			}
			prows.Close()
		}
		ssrows, _ := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, sync_status, last_positions_sync_at
			FROM trader_sync_state
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if ssrows != nil {
			for ssrows.Next() {
				var vid uuid.UUID
				var addr string
				var status string
				var last *time.Time
				if err := ssrows.Scan(&vid, &addr, &status, &last); err == nil {
					k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
					syncStatus[k] = status
					syncLast[k] = last
				}
			}
			ssrows.Close()
		}
		// latest equity fallback
		erows, _ := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, stat_date, end_equity
			FROM trader_equity_daily
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if erows != nil {
			type ed struct {
				date time.Time
				end  *float64
			}
			byWallet := map[walletKey][]ed{}
			for erows.Next() {
				var vid uuid.UUID
				var addr string
				var d time.Time
				var e *float64
				if err := erows.Scan(&vid, &addr, &d, &e); err == nil {
					k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
					byWallet[k] = append(byWallet[k], ed{date: d, end: e})
				}
			}
			erows.Close()
			for k, days := range byWallet {
				var latest time.Time
				var val *float64
				for _, d := range days {
					if d.end != nil && (val == nil || d.date.After(latest)) {
						v := *d.end
						val = &v
						latest = d.date
					}
				}
				latestEquityEnd[k] = val
			}
		}
	}

	stats := &OverviewStats{
		TotalGroups:   len(groupIDs),
		UniqueWallets: len(allKeys),
		AsOf:          time.Now().UTC(),
	}

	var accountSum float64
	hasAccount := false
	var unrealizedSum float64
	hasUnrealized := false
	missing := 0
	for _, wl := range allKeys {
		av := summaryAV[wl]
		if av == nil {
			av = latestEquityEnd[wl]
		}
		upnl := positionsUPNL[wl]
		posCount := positionsCount[wl]
		hasUsable := false
		if av != nil {
			accountSum += *av
			hasAccount = true
			hasUsable = true
		}
		if upnl != nil {
			unrealizedSum += *upnl
			hasUnrealized = true
			hasUsable = true
		}
		if posCount > 0 {
			hasUsable = true
		}
		if !hasUsable {
			missing++
		}
	}

	if hasAccount {
		v := accountSum
		stats.AccountValue = &v
	}
	if hasUnrealized {
		v := unrealizedSum
		stats.UnrealizedPnL = &v
	}
	stats.MissingWallets = missing
	stats.Partial = missing > 0

	// stale/empty groups
	empty := 0
	stale := 0
	for _, gid := range groupIDs {
		wallets := groupWallets[gid]
		if len(wallets) == 0 {
			empty++
			continue
		}
		membersWithData := 0
		var newest time.Time
		hasTS := false
		for _, wl := range wallets {
			av := summaryAV[wl]
			if av == nil {
				av = latestEquityEnd[wl]
			}
			upnl := positionsUPNL[wl]
			posCount := positionsCount[wl]
			hasUsable := (av != nil) || (upnl != nil) || (posCount > 0)
			if hasUsable {
				membersWithData++
			}
			last := syncLast[wl]
			if hasUsable && last != nil {
				if !hasTS || last.After(newest) {
					newest = *last
					hasTS = true
				}
			}
		}
		if membersWithData == 0 {
			// error group, not stale
			continue
		}
		if !hasTS || time.Since(newest) > 24*time.Hour {
			stale++
		}
	}
	stats.EmptyGroups = empty
	stats.StaleGroups = stale
	return stats, nil
}

// Stats computes group analytics (contract A6.4).
func (r *Repository) Stats(ctx context.Context, groupID, userID uuid.UUID, period string) (*GroupStats, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}
	if period == "" {
		period = trader.Period30D
	}
	now := time.Now().UTC()
	days := 30
	switch period {
	case trader.Period1D:
		days = 1
	case trader.Period7D:
		days = 7
	case trader.Period30D:
		days = 30
	case "90D":
		days = 90
	case trader.PeriodALL:
		days = 400
	}
	fromDay := now.AddDate(0, 0, -days).Truncate(24 * time.Hour)
	toDay := now.Truncate(24 * time.Hour)

	// members
	mrows, err := r.pool.Query(ctx, `SELECT venue_id, wallet_address FROM trader_group_members WHERE group_id = $1`, groupID)
	if err != nil {
		return nil, err
	}
	var wallets []walletKey
	for mrows.Next() {
		var vid uuid.UUID
		var addr string
		if err := mrows.Scan(&vid, &addr); err != nil {
			mrows.Close()
			return nil, err
		}
		wallets = append(wallets, walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))})
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}
	wallets = uniqueKeys(wallets)
	memberCount := len(wallets)

	stats := &GroupStats{
		Period:             period,
		From:               fromDay,
		To:                 toDay,
		EvaluatedAt:        now,
		MembersTotal:       memberCount,
		OpenPositions:      0,
		DefinitionsVersion: 2,
		Wallets:            []GroupStatWallet{},
	}
	if memberCount == 0 {
		stats.DataStatus = "empty"
		stats.Partial = false
		stats.MissingWallets = 0
		return stats, nil
	}

	// batch data
	summaryAV := map[walletKey]*float64{}
	positionsUPNL := map[walletKey]*float64{}
	positionsCount := map[walletKey]int{}
	positionNtl := map[walletKey]float64{}
	hasPositionRows := map[walletKey]bool{}
	syncStatus := map[walletKey]string{}
	syncLast := map[walletKey]*time.Time{}
	equityDays := map[walletKey]map[time.Time]struct {
		start float64
		end   float64
		has   bool
	}{}
	equityInWindow := map[walletKey]bool{}
	latestEquityEnd := map[walletKey]*float64{}

	// daily stats aggregates
	type dailyAgg struct {
		realized *float64
		volume   *float64
		trades   int64
		wins     int64
		losses   int64
		be       int64
		gp       float64
		gl       float64
		has      bool
	}
	dailyByWallet := map[walletKey]*dailyAgg{}
	// realized/volume/trades totals
	var realizedSum float64
	hasRealized := false
	var volumeSum float64
	hasVolume := false
	var tradeCountSum int64
	hasTradeCount := false
	var winSum, lossSum, beSum int64
	hasWinDen := false
	hasUnrealized := false
	var gpSum, glSum float64
	hasGL := false

	vids, addrs := splitKeys(wallets)

	// summary
	if len(vids) > 0 {
		srows, _ := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, account_value
			FROM trader_position_summary
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if srows != nil {
			for srows.Next() {
				var vid uuid.UUID
				var addr string
				var av *float64
				if err := srows.Scan(&vid, &addr, &av); err == nil {
					summaryAV[walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}] = av
				}
			}
			srows.Close()
		}
		// positions
		prows, _ := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, unrealized_pnl, position_value
			FROM trader_positions
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if prows != nil {
			for prows.Next() {
				var vid uuid.UUID
				var addr string
				var upnl *float64
				var pv *float64
				if err := prows.Scan(&vid, &addr, &upnl, &pv); err == nil {
					k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
					hasPositionRows[k] = true
					if upnl != nil {
						if positionsUPNL[k] == nil {
							v := 0.0
							positionsUPNL[k] = &v
						}
						*positionsUPNL[k] += *upnl
					}
					positionsCount[k]++
					if pv != nil {
						if pvv := math.Abs(*pv); true {
							positionNtl[k] += pvv
						}
					}
				}
			}
			prows.Close()
		}
		// sync state
		ssrows, _ := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, sync_status, last_positions_sync_at
			FROM trader_sync_state
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if ssrows != nil {
			for ssrows.Next() {
				var vid uuid.UUID
				var addr string
				var status string
				var last *time.Time
				if err := ssrows.Scan(&vid, &addr, &status, &last); err == nil {
					k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
					syncStatus[k] = status
					syncLast[k] = last
				}
			}
			ssrows.Close()
		}
		// daily stats
		dsrows, _ := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, stat_date, realized_pnl, volume, trade_count,
				win_count, loss_count, breakeven_count, gross_profit, gross_loss
			FROM trader_daily_stats
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))
			  AND stat_date >= $3 AND stat_date <= $4`, vids, addrs, fromDay, toDay)
		if dsrows != nil {
			for dsrows.Next() {
				var vid uuid.UUID
				var addr string
				var d time.Time
				var realized, volume, gp, gl *float64
				var trades, wins, losses, be int64
				if err := dsrows.Scan(&vid, &addr, &d, &realized, &volume, &trades, &wins, &losses, &be, &gp, &gl); err == nil {
					k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
					if dailyByWallet[k] == nil {
						dailyByWallet[k] = &dailyAgg{}
					}
					da := dailyByWallet[k]
					da.has = true
					if realized != nil {
						if da.realized == nil {
							v := 0.0
							da.realized = &v
						}
						*da.realized += *realized
					}
					if volume != nil {
						if da.volume == nil {
							v := 0.0
							da.volume = &v
						}
						*da.volume += *volume
					}
					da.trades += trades
					da.wins += wins
					da.losses += losses
					da.be += be
					if gp != nil {
						da.gp += *gp
					}
					if gl != nil {
						da.gl += *gl
					}
					// global
					if realized != nil {
						realizedSum += *realized
						hasRealized = true
					}
					if volume != nil {
						volumeSum += *volume
						hasVolume = true
					}
					tradeCountSum += trades
					hasTradeCount = true
					winSum += wins
					lossSum += losses
					beSum += be
					hasWinDen = true
					if gp != nil {
						gpSum += *gp
					}
					if gl != nil {
						glSum += *gl
						hasGL = true
					}
				}
			}
			dsrows.Close()
		}
		// equity daily
		erows, _ := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, stat_date, start_equity, end_equity
			FROM trader_equity_daily
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if erows != nil {
			for erows.Next() {
				var vid uuid.UUID
				var addr string
				var d time.Time
				var s, e *float64
				if err := erows.Scan(&vid, &addr, &d, &s, &e); err == nil {
					k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
					if equityDays[k] == nil {
						equityDays[k] = map[time.Time]struct {
							start float64
							end   float64
							has   bool
						}{}
					}
					if s != nil && e != nil {
						entry := equityDays[k][d]
						entry.start += *s
						entry.end += *e
						entry.has = true
						equityDays[k][d] = entry
						if !d.Before(fromDay) && !d.After(toDay) {
							equityInWindow[k] = true
						}
					}
					if e != nil {
						if cur, ok := latestEquityEnd[k]; !ok || cur == nil {
							v := *e
							latestEquityEnd[k] = &v
						}
					}
				}
			}
			erows.Close()
		}
		// fix latestEquityEnd to max date
		latestEquityEnd = map[walletKey]*float64{}
		for k, days := range equityDays {
			var latest time.Time
			var val *float64
			for d, v := range days {
				if v.has && (val == nil || d.After(latest)) {
					end := v.end
					val = &end
					latest = d
				}
			}
			latestEquityEnd[k] = val
		}
	}

	// aggregate
	var accountValueSum float64
	hasAccountValue := false
	var unrealizedSum float64
	var totalNotionalSum float64
	hasPositionData := false
	var openPosSum int
	missing := 0
	membersWithData := 0
	allReady := true
	var newest time.Time
	hasTS := false
	anyStale := false

	venueCodes := map[uuid.UUID]string{}
	if vrows, err := r.pool.Query(ctx, `SELECT id, code FROM venues`); err == nil {
		for vrows.Next() {
			var vid uuid.UUID
			var code string
			if err := vrows.Scan(&vid, &code); err == nil {
				venueCodes[vid] = code
			}
		}
		vrows.Close()
	}

	walletFreshness := []GroupStatWallet{}

	// TWR day aggregation across wallets
	dayAgg := map[time.Time]struct {
		start float64
		end   float64
		has   bool
	}{}
	dayFlow := map[time.Time]float64{}
	flowsOK := true

	for _, wl := range wallets {
		k := wl
		av := summaryAV[k]
		if av == nil {
			av = latestEquityEnd[k]
		}
		hasWalletData := false
		if av != nil {
			accountValueSum += *av
			hasAccountValue = true
			hasWalletData = true
		}
		upnl := positionsUPNL[k]
		if upnl != nil {
			unrealizedSum += *upnl
			hasUnrealized = true
			hasWalletData = true
		}
		if hasPositionRows[k] {
			hasPositionData = true
			hasWalletData = true
			totalNotionalSum += positionNtl[k]
			openPosSum += positionsCount[k]
		}
		hasEquity := equityInWindow[k]
		fullData := hasWalletData && hasEquity
		// sync
		ss := syncStatus[k]
		last := syncLast[k]
		ready := ss == "ready"
		if last != nil {
			if time.Since(*last) > 15*time.Minute {
				ready = false
			}
		} else if ss == "ready" {
			ready = false
		}
		if ss != "ready" {
			ready = false
		}
		if !fullData {
			ready = false
		}
		if !ready {
			allReady = false
		}
		if fullData {
			if last != nil {
				if time.Since(*last) > 24*time.Hour {
					anyStale = true
				}
				if !hasTS || last.After(newest) {
					newest = *last
					hasTS = true
				}
			} else {
				anyStale = true
			}
		}
		if !fullData {
			missing++
		} else {
			membersWithData++
		}
		// freshness
		hasPos := hasPositionRows[k]
		hasEq := equityInWindow[k]
		reason := (*string)(nil)
		if !fullData {
			r := "incomplete data"
			reason = &r
			if !hasWalletData {
				r := "no wallet data"
				reason = &r
			}
		}
		vc := venueCodes[k.venueID]
		if vc == "" {
			vc = "unknown"
		}
		wf := GroupStatWallet{
			WalletAddress: k.addr,
			Venue:         vc,
			DataStatus:    "ready",
			HasPositions:  hasPos,
			HasEquity:     hasEq,
			MissingReason: reason,
		}
		if !fullData {
			wf.DataStatus = "partial"
		}
		if ss != "ready" || !fullData {
			if ss == "error" {
				wf.DataStatus = "error"
			} else if !fullData {
				wf.DataStatus = "partial"
			}
		}
		if syncLast[k] != nil {
			tt := *syncLast[k]
			wf.AsOf = &tt
			if time.Since(tt) > 24*time.Hour {
				wf.DataStatus = "stale"
			}
		}
		walletFreshness = append(walletFreshness, wf)

		// TWR
		if equityDays[k] != nil {
			for d, v := range equityDays[k] {
				if !v.has {
					continue
				}
				entry := dayAgg[d]
				entry.start += v.start
				entry.end += v.end
				entry.has = true
				dayAgg[d] = entry
			}
		}
		// ledger cash flows
		if r.transferFetcher != nil {
			cf, ok := r.walletCashFlows(ctx, k, days)
			if !ok {
				flowsOK = false
			} else {
				for d, v := range cf {
					dayFlow[d] += v
				}
			}
		}
	}

	// anyStale: if newest data >24h
	if membersWithData > 0 {
		if !hasTS || time.Since(newest) > 24*time.Hour {
			anyStale = true
		}
		if hasTS {
			tt := newest
			stats.AsOf = &tt
		}
	}
	stats.Wallets = walletFreshness

	// TWR chain + roi_index series
	var points []TWREquityPoint
	for d, v := range dayAgg {
		if d.Before(fromDay) {
			continue
		}
		if !v.has {
			continue
		}
		end := v.end - dayFlow[d]
		pt := TWREquityPoint{Date: d, Start: v.start, End: end, Valid: v.start > 0}
		points = append(points, pt)
	}
	// sort points by date for index
	sort.Slice(points, func(i, j int) bool { return points[i].Date.Before(points[j].Date) })
	roi := TWRChain(points)
	// roi_index series
	var indexSeries []float64
	cum := 1.0
	hasIndex := false
	for _, p := range points {
		if !p.Valid || p.Start <= 0 {
			// skip invalid days in chain? TWR skips; index unchanged?
			continue
		}
		r := p.End/p.Start - 1.0
		cum *= (1.0 + r)
		indexSeries = append(indexSeries, cum*100.0)
		hasIndex = true
	}
	var maxDD *float64
	if hasIndex && len(indexSeries) > 0 {
		maxDD = MaxDrawdownPct(indexSeries)
	}

	// fill stats
	if hasAccountValue {
		v := accountValueSum
		stats.AccountValue = &v
	}
	if hasPositionData {
		v := totalNotionalSum
		stats.TotalNotional = &v
	}
	if hasUnrealized {
		v2 := unrealizedSum
		stats.UnrealizedPnL = &v2
	}
	if hasRealized {
		v := realizedSum
		stats.RealizedPnL = &v
	}
	allHaveEquity := true
	for _, wl := range wallets {
		if !equityInWindow[wl] {
			allHaveEquity = false
			break
		}
	}
	if allHaveEquity && flowsOK {
		stats.ROI = roi
		stats.MaxDrawdownPct = maxDD
	} else {
		stats.ROI = nil
		stats.MaxDrawdownPct = nil
	}
	if hasWinDen && (winSum+lossSum) > 0 {
		v := float64(winSum) / float64(winSum+lossSum)
		stats.WinRate = &v
	}
	if hasVolume {
		v := volumeSum
		stats.Volume = &v
	}
	if hasGL && glSum != 0 && lossSum > 0 && tradeCountSum >= 10 {
		v := gpSum / math.Abs(glSum)
		stats.ProfitFactor = &v
	}
	// win/loss/breakeven counts
	wcs, lcs, bcs := winSum, lossSum, beSum
	stats.WinCount = &wcs
	stats.LossCount = &lcs
	stats.BreakevenCount = &bcs
	stats.OpenPositions = openPosSum
	if hasTradeCount {
		v := tradeCountSum
		stats.TradeCount = &v
	}
	stats.MissingWallets = missing
	stats.MembersWithData = membersWithData
	stats.Partial = missing > 0 || !allReady || anyStale
	stats.DataStatus = DeriveGroupStatus(memberCount, membersWithData, allReady, anyStale)
	return stats, nil
}

// Positions computes group positions (contract A6.6).
func (r *Repository) Positions(ctx context.Context, groupID, userID uuid.UUID) (*PositionsResponse, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	mrows, err := r.pool.Query(ctx, `SELECT venue_id, wallet_address FROM trader_group_members WHERE group_id = $1`, groupID)
	if err != nil {
		return nil, err
	}
	var wallets []walletKey
	for mrows.Next() {
		var vid uuid.UUID
		var addr string
		if err := mrows.Scan(&vid, &addr); err != nil {
			mrows.Close()
			return nil, err
		}
		wallets = append(wallets, walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))})
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}
	wallets = uniqueKeys(wallets)
	memberCount := len(wallets)

	resp := &PositionsResponse{
		AsOf:      now,
		Positions: []PositionRow{},
		Totals:    PositionTotals{},
	}
	if memberCount == 0 {
		resp.DataStatus = "empty"
		resp.Partial = false
		resp.MissingWallets = 0
		return resp, nil
	}

	vids, addrs := splitKeys(wallets)
	// fetch positions
	type posRow struct {
		venueID  uuid.UUID
		addr     string
		coin     string
		side     string
		size     float64
		entry    *float64
		mark     *float64
		posValue *float64
		upnl     *float64
		leverage *float64
		liq      *float64
		asOf     time.Time
	}
	var positions []posRow
	if len(vids) > 0 {
		prows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, coin, side, size, entry_price, mark_price, position_value, unrealized_pnl, leverage, liquidation_price, as_of
			FROM trader_positions
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if err != nil {
			return nil, err
		}
		for prows.Next() {
			var pr posRow
			if err := prows.Scan(&pr.venueID, &pr.addr, &pr.coin, &pr.side, &pr.size, &pr.entry, &pr.mark, &pr.posValue, &pr.upnl, &pr.leverage, &pr.liq, &pr.asOf); err != nil {
				prows.Close()
				return nil, err
			}
			pr.addr = strings.ToLower(strings.TrimSpace(pr.addr))
			positions = append(positions, pr)
		}
		prows.Close()
		if err := prows.Err(); err != nil {
			return nil, err
		}
	}

	// sync state for stale
	syncLast := map[walletKey]*time.Time{}
	if len(vids) > 0 {
		ssrows, _ := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, last_positions_sync_at
			FROM trader_sync_state
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if ssrows != nil {
			for ssrows.Next() {
				var vid uuid.UUID
				var addr string
				var last *time.Time
				if err := ssrows.Scan(&vid, &addr, &last); err == nil {
					syncLast[walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}] = last
				}
			}
			ssrows.Close()
		}
	}

	// display name + alias maps
	displayNameMap := map[walletKey]*string{}
	aliasMap := map[walletKey]*string{}
	if len(vids) > 0 {
		drows, _ := r.pool.Query(ctx, `SELECT venue_id, wallet_address, display_name FROM trader_registry WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if drows != nil {
			for drows.Next() {
				var vid uuid.UUID
				var addr string
				var dn *string
				if err := drows.Scan(&vid, &addr, &dn); err == nil {
					displayNameMap[walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}] = dn
				}
			}
			drows.Close()
		}
		arows, _ := r.pool.Query(ctx, `SELECT venue_id, wallet_address, alias FROM trader_group_members WHERE group_id = $1`, groupID)
		if arows != nil {
			for arows.Next() {
				var vid uuid.UUID
				var addr string
				var alias string
				if err := arows.Scan(&vid, &addr, &alias); err == nil {
					a := alias
					if strings.TrimSpace(a) == "" {
						aliasMap[walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}] = nil
					} else {
						aliasMap[walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}] = &a
					}
				}
			}
			arows.Close()
		}
	}

	// group positions by coin
	coinMap := map[string]*PositionRow{}
	coinOrder := []string{}
	walletHasPos := map[walletKey]bool{}
	walletHasUnrealized := map[walletKey]bool{}
	for _, pr := range positions {
		k := walletKey{venueID: pr.venueID, addr: pr.addr}
		walletHasPos[k] = true
		if pr.upnl != nil {
			walletHasUnrealized[k] = true
		}
		row, ok := coinMap[pr.coin]
		if !ok {
			row = &PositionRow{Coin: pr.coin, Wallets: []PositionWallet{}}
			coinMap[pr.coin] = row
			coinOrder = append(coinOrder, pr.coin)
		}
		// ntl
		ntl := 0.0
		if pr.posValue != nil {
			ntl = math.Abs(*pr.posValue)
		}
		if pr.side == "LONG" {
			row.LongNtl += ntl
		} else {
			row.ShortNtl += ntl
		}
		row.WalletCount++ // counts rows; distinct wallets? contract wallet_count distinct wallets. We'll adjust below.
		if pr.upnl != nil {
			if row.UnrealizedPnL == nil {
				v := 0.0
				row.UnrealizedPnL = &v
			}
			*row.UnrealizedPnL += *pr.upnl
		}
		stale := false
		if time.Since(pr.asOf) > 15*time.Minute {
			stale = true
		}
		var dist *float64
		if pr.mark != nil && pr.liq != nil && *pr.mark != 0 {
			v := math.Abs(*pr.mark-*pr.liq) / math.Abs(*pr.mark) * 100.0
			dist = &v
		}
		row.Wallets = append(row.Wallets, PositionWallet{
			WalletAddress:            pr.addr,
			DisplayName:              displayNameMap[k],
			Alias:                    aliasMap[k],
			Side:                     pr.side,
			Size:                     pr.size,
			EntryPrice:               pr.entry,
			MarkPrice:                pr.mark,
			Leverage:                 pr.leverage,
			LiquidationPrice:         pr.liq,
			DistanceToLiquidationPct: dist,
			PositionValue:            pr.posValue,
			UnrealizedPnL:            pr.upnl,
			AsOf:                     pr.asOf.UTC(),
			Stale:                    stale,
		})
	}
	// fix wallet_count distinct per coin
	for _, coin := range coinOrder {
		row := coinMap[coin]
		distinct := map[string]bool{}
		for _, w := range row.Wallets {
			distinct[w.WalletAddress] = true
		}
		row.WalletCount = len(distinct)
		row.NetNtl = row.LongNtl - row.ShortNtl
		row.GrossNtl = row.LongNtl + row.ShortNtl
	}

	// totals
	var longTotal, shortTotal float64
	openPosTotal := len(positions)
	for _, pr := range positions {
		ntl := 0.0
		if pr.posValue != nil {
			ntl = math.Abs(*pr.posValue)
		}
		if pr.side == "LONG" {
			longTotal += ntl
		} else {
			shortTotal += ntl
		}
	}
	resp.Totals.LongNtl = longTotal
	resp.Totals.ShortNtl = shortTotal
	resp.Totals.NetNtl = longTotal - shortTotal
	resp.Totals.GrossNtl = longTotal + shortTotal
	resp.Totals.OpenPositions = openPosTotal

	// positions slice
	for _, coin := range coinOrder {
		resp.Positions = append(resp.Positions, *coinMap[coin])
	}

	// missing wallets
	missing := 0
	membersWithData := 0
	allReady := true
	var newest time.Time
	hasTS := false
	anyStale := false
	for _, wl := range wallets {
		k := wl
		if !walletHasPos[k] || !walletHasUnrealized[k] {
			missing++
			continue
		}
		membersWithData++
		last := syncLast[k]
		ready := true
		if last != nil {
			if time.Since(*last) > 15*time.Minute {
				ready = false
			}
			if !hasTS || last.After(newest) {
				newest = *last
				hasTS = true
			}
		} else {
			ready = false
		}
		if !ready {
			allReady = false
		}
		if last != nil && time.Since(*last) > 24*time.Hour {
			anyStale = true
		}
	}
	if membersWithData > 0 {
		if !hasTS || time.Since(newest) > 24*time.Hour {
			anyStale = true
		}
	}
	resp.MissingWallets = missing
	resp.Partial = missing > 0
	resp.DataStatus = DeriveGroupStatus(memberCount, membersWithData, allReady, anyStale)
	return resp, nil
}

// Performance computes group performance series (contract A6.5).
func (r *Repository) Performance(ctx context.Context, groupID, userID uuid.UUID, period string) (*PerformanceResponse, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}
	if period == "" {
		period = trader.Period30D
	}
	now := time.Now().UTC()
	days := 30
	intraday := period == trader.Period1D
	switch period {
	case trader.Period1D:
		days = 1
	case trader.Period7D:
		days = 7
	case trader.Period30D:
		days = 30
	case "90D":
		days = 90
	case trader.PeriodALL:
		days = 400
	}
	fromDay := now.AddDate(0, 0, -days).Truncate(24 * time.Hour)
	toDay := now.Truncate(24 * time.Hour)

	mrows, err := r.pool.Query(ctx, `SELECT venue_id, wallet_address FROM trader_group_members WHERE group_id = $1`, groupID)
	if err != nil {
		return nil, err
	}
	var wallets []walletKey
	for mrows.Next() {
		var vid uuid.UUID
		var addr string
		if err := mrows.Scan(&vid, &addr); err != nil {
			mrows.Close()
			return nil, err
		}
		wallets = append(wallets, walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))})
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}
	wallets = uniqueKeys(wallets)
	memberCount := len(wallets)

	resp := &PerformanceResponse{
		Period:      period,
		Granularity: "daily",
		From:        fromDay,
		To:          toDay,
		Points:      []PerformancePoint{},
	}
	if intraday {
		resp.Granularity = "intraday"
		resp.From = now.Add(-24 * time.Hour)
		resp.To = now
	}
	// (PerformanceResponse has no DataStatus field per contract)
	missing := 0
	membersWithData := 0

	if memberCount == 0 {
		resp.Partial = false
		resp.MissingWallets = 0
		resp.InsufficientData = true
		return resp, nil
	}

	// Group wallets by venue for intraday/equity queries
	venues := map[uuid.UUID][]string{}
	for _, wl := range wallets {
		venues[wl.venueID] = append(venues[wl.venueID], wl.addr)
	}

	if intraday {
		// fetch intraday points per venue
		type aggPoint struct {
			t   time.Time
			av  float64
			has bool
		}
		byTime := map[time.Time]*aggPoint{}
		timeOrder := []time.Time{}
		for vid, addrs := range venues {
			pts, err := trader.NewRepository(r.pool).ListEquityIntraday(ctx, vid, addrs, resp.From, resp.To)
			if err != nil {
				return nil, err
			}
			for _, p := range pts {
				if byTime[p.SampledAt] == nil {
					ap := &aggPoint{t: p.SampledAt}
					byTime[p.SampledAt] = ap
					timeOrder = append(timeOrder, p.SampledAt)
				}
				if p.AccountValue != nil {
					byTime[p.SampledAt].av += *p.AccountValue
					byTime[p.SampledAt].has = true
				}
			}
		}
		// unique times sorted
		timeSet := map[time.Time]bool{}
		var times []time.Time
		for t := range byTime {
			if !timeSet[t] {
				timeSet[t] = true
				times = append(times, t)
			}
		}
		sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })

		// missing wallets: wallets with no intraday points
		walletHasIntraday := map[walletKey]bool{}
		for vid, addrs := range venues {
			pts, _ := trader.NewRepository(r.pool).ListEquityIntraday(ctx, vid, addrs, resp.From, resp.To)
			for _, p := range pts {
				walletHasIntraday[walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(p.WalletAddress))}] = true
			}
		}
		for _, wl := range wallets {
			if !walletHasIntraday[wl] {
				missing++
			} else {
				membersWithData++
			}
		}

		// build points with roi_index sequential
		var prevAV *float64
		cum := 1.0
		hasCum := false
		for _, t := range times {
			ap := byTime[t]
			pt := PerformancePoint{T: t.UTC()}
			if ap.has {
				v := ap.av
				pt.AccountValue = &v
			}
			// roi_index
			if ap.has {
				if prevAV != nil && *prevAV > 0 {
					cum *= ap.av / *prevAV
					hasCum = true
				}
				if hasCum || len(times) > 0 {
					// first point index = 100 (baseline) if no prev
					if prevAV == nil {
						v := 100.0
						pt.ROIIndex = &v
						hasCum = true
						cum = 1.0
					} else {
						v := cum * 100.0
						pt.ROIIndex = &v
					}
				}
				vv := ap.av
				prevAV = &vv
			} else {
				prevAV = nil
			}
			if pt.AccountValue != nil || pt.ROIIndex != nil {
				resp.Points = append(resp.Points, pt)
			}
		}
		resp.MissingWallets = missing
		resp.Partial = missing > 0
		resp.InsufficientData = len(resp.Points) < 2
		ma := PerformanceMetricAvailability{}
		for _, p := range resp.Points {
			if p.AccountValue != nil {
				ma.AccountValue = true
			}
			if p.ROIIndex != nil {
				ma.ROI = true
			}
		}
		// cumulative realized pnl always null intraday
		resp.MetricAvailability = ma
		return resp, nil
	}

	// daily path
	// fetch equity daily rows per wallet
	type dayKey struct {
		date time.Time
	}
	// aggregate by date
	dayStart := map[time.Time]float64{}
	dayEnd := map[time.Time]float64{}
	dayHas := map[time.Time]bool{}
	dayStartHas := map[time.Time]bool{}
	dayRealized := map[time.Time]float64{}
	dayHasRealized := map[time.Time]bool{}
	dayHasAny := map[time.Time]bool{}

	walletHasData := map[walletKey]bool{}

	for _, wl := range wallets {
		// equity daily rows
		rows, err := r.pool.Query(ctx, `
			SELECT stat_date, start_equity, end_equity
			FROM trader_equity_daily
			WHERE venue_id = $1 AND wallet_address = $2
			  AND stat_date >= $3 AND stat_date <= $4
			ORDER BY stat_date ASC`, wl.venueID, wl.addr, fromDay, toDay)
		if err != nil {
			return nil, err
		}
		hasEq := false
		for rows.Next() {
			var d time.Time
			var s, e *float64
			if err := rows.Scan(&d, &s, &e); err != nil {
				rows.Close()
				return nil, err
			}
			d = d.UTC().Truncate(24 * time.Hour)
			dayHasAny[d] = true
			if s != nil && e != nil {
				dayStart[d] += *s
				dayEnd[d] += *e
				dayHas[d] = true
				dayStartHas[d] = true
				hasEq = true
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		// daily stats
		dsrows, err := r.pool.Query(ctx, `
			SELECT stat_date, realized_pnl
			FROM trader_daily_stats
			WHERE venue_id = $1 AND wallet_address = $2
			  AND stat_date >= $3 AND stat_date <= $4`, wl.venueID, wl.addr, fromDay, toDay)
		if err != nil {
			return nil, err
		}
		hasDs := false
		for dsrows.Next() {
			var d time.Time
			var realized *float64
			if err := dsrows.Scan(&d, &realized); err != nil {
				dsrows.Close()
				return nil, err
			}
			d = d.UTC().Truncate(24 * time.Hour)
			dayHasAny[d] = true
			if realized != nil {
				dayRealized[d] += *realized
				dayHasRealized[d] = true
				hasDs = true
			}
		}
		dsrows.Close()
		if err := dsrows.Err(); err != nil {
			return nil, err
		}
		if hasEq || hasDs {
			walletHasData[wl] = true
		}
	}

	for _, wl := range wallets {
		if !walletHasData[wl] {
			missing++
		} else {
			membersWithData++
		}
	}

	// build sorted dates
	var dates []time.Time
	for d := range dayHasAny {
		dates = append(dates, d)
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })

	// cumulative realized
	cumRealized := 0.0
	hasCumRealized := false
	// roi_index cumulative
	cumROI := 1.0
	hasCumROI := false

	for _, d := range dates {
		pt := PerformancePoint{T: d.UTC()}
		if dayHas[d] {
			v := dayEnd[d]
			pt.AccountValue = &v
		}
		// cumulative realized pnl
		if dayHasRealized[d] {
			cumRealized += dayRealized[d]
			hasCumRealized = true
		}
		if hasCumRealized {
			v := cumRealized
			pt.CumulativeRealizedPnL = &v
		} else {
			// if no realized rows up to this date, null
			pt.CumulativeRealizedPnL = nil
		}
		// roi_index
		if dayStartHas[d] && dayStart[d] > 0 {
			r := dayEnd[d]/dayStart[d] - 1.0
			if !hasCumROI {
				hasCumROI = true
				cumROI = 1.0
			}
			cumROI *= (1.0 + r)
			v := cumROI * 100.0
			pt.ROIIndex = &v
		} else {
			// if no valid day, leave null (but keep cumROI unchanged)
			if hasCumROI {
				// carry? Contract null when no valid day. We'll leave null for invalid day.
			}
		}
		if pt.AccountValue != nil || pt.CumulativeRealizedPnL != nil || pt.ROIIndex != nil {
			resp.Points = append(resp.Points, pt)
		}
	}

	resp.MissingWallets = missing
	resp.Partial = missing > 0
	resp.InsufficientData = len(resp.Points) < 2

	ma := PerformanceMetricAvailability{}
	for _, p := range resp.Points {
		if p.AccountValue != nil {
			ma.AccountValue = true
		}
		if p.CumulativeRealizedPnL != nil {
			ma.CumulativeRealizedPnL = true
		}
		if p.ROIIndex != nil {
			ma.ROI = true
		}
	}
	resp.MetricAvailability = ma
	return resp, nil
}

// Compare computes comparison across groups (contract A6.8).
func (r *Repository) Compare(ctx context.Context, userID uuid.UUID, groupIDs []uuid.UUID, period string) (*CompareResponse, error) {
	if len(groupIDs) < 2 {
		return nil, domain.NewError(domain.ErrCodeCompareTooFew, "compare requires at least 2 groups")
	}
	if len(groupIDs) > 10 {
		return nil, domain.NewError(domain.ErrCodeCompareTooMany, "compare allows at most 10 groups")
	}
	if period == "" {
		period = trader.Period30D
	}
	now := time.Now().UTC()
	// fetch members per group
	groupMembers := map[uuid.UUID][]walletKey{}
	groupName := map[uuid.UUID]string{}
	groupMemberCount := map[uuid.UUID]int{}
	for _, gid := range groupIDs {
		if err := r.requireOwner(ctx, gid, userID); err != nil {
			return nil, err
		}
		var name string
		if err := r.pool.QueryRow(ctx, `SELECT name FROM trader_groups WHERE id = $1`, gid).Scan(&name); err != nil {
			return nil, err
		}
		groupName[gid] = name
		mrows, err := r.pool.Query(ctx, `SELECT venue_id, wallet_address FROM trader_group_members WHERE group_id = $1`, gid)
		if err != nil {
			return nil, err
		}
		var wls []walletKey
		for mrows.Next() {
			var vid uuid.UUID
			var addr string
			if err := mrows.Scan(&vid, &addr); err != nil {
				mrows.Close()
				return nil, err
			}
			wls = append(wls, walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))})
		}
		mrows.Close()
		if err := mrows.Err(); err != nil {
			return nil, err
		}
		wls = uniqueKeys(wls)
		groupMembers[gid] = wls
		groupMemberCount[gid] = len(wls)
	}

	resp := &CompareResponse{
		Period:   period,
		Groups:   []CompareGroup{},
		Series:   []CompareSeries{},
		Overlap:  CompareOverlap{Pairwise: []CompareOverlapPair{}},
		Warnings: []string{},
		AsOf:     now,
	}

	// stats + series per group
	for _, gid := range groupIDs {
		st, err := r.Stats(ctx, gid, userID, period)
		if err != nil {
			return nil, err
		}
		resp.Groups = append(resp.Groups, CompareGroup{
			ID:          gid,
			Name:        groupName[gid],
			MemberCount: groupMemberCount[gid],
			Stats:       *st,
		})
		// series from performance
		perf, err := r.Performance(ctx, gid, userID, period)
		if err != nil {
			return nil, err
		}
		pts := []ComparePoint{}
		for _, p := range perf.Points {
			pts = append(pts, ComparePoint{T: p.T, ROIIndex: p.ROIIndex})
		}
		resp.Series = append(resp.Series, CompareSeries{GroupID: gid, Points: pts})
	}

	// overlap pairwise
	sharedAny := false
	for i := 0; i < len(groupIDs); i++ {
		for j := i + 1; j < len(groupIDs); j++ {
			a, b := groupIDs[i], groupIDs[j]
			setA := map[string]bool{}
			for _, wl := range groupMembers[a] {
				setA[wl.venueID.String()+"|"+wl.addr] = true
			}
			shared := 0
			for _, wl := range groupMembers[b] {
				if setA[wl.venueID.String()+"|"+wl.addr] {
					shared++
				}
			}
			resp.Overlap.Pairwise = append(resp.Overlap.Pairwise, CompareOverlapPair{
				A: a, B: b, SharedWallets: shared,
			})
			if shared > 0 {
				sharedAny = true
			}
		}
	}
	if sharedAny {
		resp.Warnings = append(resp.Warnings, "groups share wallets; results are not independent portfolios")
	}
	return resp, nil
}

// OwnerOf returns the group's owner (for the search group_id filter).
func (r *Repository) OwnerOf(ctx context.Context, groupID uuid.UUID) (uuid.UUID, error) {
	var owner uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT user_id FROM trader_groups WHERE id = $1`, groupID).Scan(&owner)
	if err != nil {
		if err == pgx.ErrNoRows {
			return uuid.Nil, notFound()
		}
		return uuid.Nil, err
	}
	return owner, nil
}

// VenueIDByCode resolves a venue code to its id (unknown �+' 404).
func (r *Repository) VenueIDByCode(ctx context.Context, code string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM venues WHERE code = $1`, code).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return uuid.Nil, domain.NewError(domain.ErrCodeNotFound, "unknown venue: "+code)
		}
		return uuid.Nil, err
	}
	return id, nil
}

// periodRow mirrors trader.PeriodMetrics with nullable holders.
type periodRow struct {
	Period             *string
	AsOf               *time.Time
	PnL                *float64
	RealizedPnL        *float64
	ROI                *float64
	WinRate            *float64
	TradeCount         *int64
	Volume             *float64
	GrossProfit        *float64
	GrossLoss          *float64
	ProfitFactor       *float64
	AvgTradePnL        *float64
	LongCount          *int64
	LongWins           *int64
	ShortCount         *int64
	ShortWins          *int64
	MaxDrawdownPct     *float64
	AvgHoldingTimeSec  *float64
	LastTradeAt        *time.Time
	DataStatus         *string
	IsPartial          *bool
	CalculationVersion *int
}

func (row periodRow) toMetrics(venueID uuid.UUID, venue, addr string, display *string) *trader.PeriodMetrics {
	if row.Period == nil {
		return nil
	}
	m := &trader.PeriodMetrics{
		VenueID: venueID, Venue: venue, WalletAddress: addr, DisplayName: display,
		Period: *row.Period, PnL: row.PnL, RealizedPnL: row.RealizedPnL, ROI: row.ROI,
		WinRate: row.WinRate, TradeCount: row.TradeCount, Volume: row.Volume,
		GrossProfit: row.GrossProfit, GrossLoss: row.GrossLoss,
		ProfitFactor: row.ProfitFactor, AvgTradePnL: row.AvgTradePnL,
		LongCount: row.LongCount, LongWins: row.LongWins,
		ShortCount: row.ShortCount, ShortWins: row.ShortWins,
		MaxDrawdownPct: row.MaxDrawdownPct, AvgHoldingTimeSec: row.AvgHoldingTimeSec,
		LastTradeAt: row.LastTradeAt, IsPartial: row.IsPartial != nil && *row.IsPartial,
	}
	if row.AsOf != nil {
		m.AsOf = *row.AsOf
	}
	if row.DataStatus != nil {
		m.DataStatus = trader.DataStatus(*row.DataStatus)
	}
	if row.CalculationVersion != nil {
		m.CalculationVersion = *row.CalculationVersion
	}
	return m
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate key")
}

func int64Or(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// WithCursorSecret configures the HMAC secret used for activity cursors.
func (r *Repository) WithCursorSecret(secret []byte) *Repository {
	r.cursorSecret = secret
	return r
}

// WithTransferFetcher configures the on-demand transfer fetcher.
func (r *Repository) WithTransferFetcher(fn func(ctx context.Context, venueCode, addr string, days int) (*trader.TransfersPage, error)) *Repository {
	r.transferFetcher = fn
	return r
}

// walletCashFlows fetches ledger (deposit/withdraw) cash flows for a wallet over
// the period and returns them keyed by UTC day. Returns ok=false on fetch failure.
func (r *Repository) walletCashFlows(ctx context.Context, wl walletKey, days int) (map[time.Time]float64, bool) {
	out := map[time.Time]float64{}
	if r.transferFetcher == nil {
		return out, true
	}
	var code string
	if err := r.pool.QueryRow(ctx, `SELECT code FROM venues WHERE id = $1`, wl.venueID).Scan(&code); err != nil {
		return nil, false
	}
	page, err := r.transferFetcher(ctx, code, wl.addr, days)
	if err != nil || page == nil {
		return nil, false
	}
	for _, tr := range page.Rows {
		amt := 0.0
		if tr.UsdcValue != nil {
			amt = *tr.UsdcValue
		} else if tr.Amount != nil {
			amt = *tr.Amount
		}
		cf := amt
		if tr.IsDeposit != nil {
			if !*tr.IsDeposit {
				cf = -amt
			}
		} else {
			switch tr.Type {
			case "withdraw":
				cf = -amt
			}
		}
		day := tr.Time.UTC().Truncate(24 * time.Hour)
		out[day] += cf
	}
	return out, true
}

// Exposure computes per-coin exposure (contract A6.6.1).
func (r *Repository) Exposure(ctx context.Context, groupID, userID uuid.UUID, period string) (*ExposureResponse, error) {
	if err := r.requireOwner(ctx, groupID, userID); err != nil {
		return nil, err
	}
	if period == "" {
		period = trader.Period30D
	}
	now := time.Now().UTC()
	mrows, err := r.pool.Query(ctx, `SELECT venue_id, wallet_address FROM trader_group_members WHERE group_id = $1`, groupID)
	if err != nil {
		return nil, err
	}
	var wallets []walletKey
	for mrows.Next() {
		var vid uuid.UUID
		var addr string
		if err := mrows.Scan(&vid, &addr); err != nil {
			mrows.Close()
			return nil, err
		}
		wallets = append(wallets, walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))})
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}
	wallets = uniqueKeys(wallets)
	memberCount := len(wallets)

	resp := &ExposureResponse{
		Period: period,
		AsOf:   &now,
		Rows:   []ExposureRow{},
	}
	if memberCount == 0 {
		resp.DataStatus = "empty"
		resp.Partial = false
		resp.MissingWallets = 0
		return resp, nil
	}

	vids, addrs := splitKeys(wallets)
	type posRow struct {
		venueID  uuid.UUID
		addr     string
		coin     string
		side     string
		posValue *float64
		leverage *float64
		asOf     time.Time
	}
	var positions []posRow
	if len(vids) > 0 {
		prows, err := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, coin, side, position_value, leverage, as_of
			FROM trader_positions
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if err != nil {
			return nil, err
		}
		for prows.Next() {
			var pr posRow
			if err := prows.Scan(&pr.venueID, &pr.addr, &pr.coin, &pr.side, &pr.posValue, &pr.leverage, &pr.asOf); err != nil {
				prows.Close()
				return nil, err
			}
			pr.addr = strings.ToLower(strings.TrimSpace(pr.addr))
			positions = append(positions, pr)
		}
		prows.Close()
		if err := prows.Err(); err != nil {
			return nil, err
		}
	}

	// sync state for freshness
	syncLast := map[walletKey]*time.Time{}
	syncStatus := map[walletKey]string{}
	if len(vids) > 0 {
		ssrows, _ := r.pool.Query(ctx, `
			SELECT venue_id, wallet_address, sync_status, last_positions_sync_at
			FROM trader_sync_state
			WHERE (venue_id, wallet_address) IN (SELECT unnest($1::uuid[]), unnest($2::text[]))`, vids, addrs)
		if ssrows != nil {
			for ssrows.Next() {
				var vid uuid.UUID
				var addr string
				var status string
				var last *time.Time
				if err := ssrows.Scan(&vid, &addr, &status, &last); err == nil {
					k := walletKey{venueID: vid, addr: strings.ToLower(strings.TrimSpace(addr))}
					syncStatus[k] = status
					syncLast[k] = last
				}
			}
			ssrows.Close()
		}
	}

	// aggregate by coin
	type coinAgg struct {
		longNtl      float64
		shortNtl     float64
		longWallets  map[string]bool
		shortWallets map[string]bool
		levNum       float64
		levDen       float64
		hasLev       bool
	}
	coinMap := map[string]*coinAgg{}
	coinOrder := []string{}
	walletHasPos := map[walletKey]bool{}
	for _, pr := range positions {
		k := walletKey{venueID: pr.venueID, addr: pr.addr}
		walletHasPos[k] = true
		ntl := 0.0
		if pr.posValue != nil {
			ntl = math.Abs(*pr.posValue)
		}
		ca, ok := coinMap[pr.coin]
		if !ok {
			ca = &coinAgg{longWallets: map[string]bool{}, shortWallets: map[string]bool{}}
			coinMap[pr.coin] = ca
			coinOrder = append(coinOrder, pr.coin)
		}
		if pr.side == "LONG" {
			ca.longNtl += ntl
			ca.longWallets[pr.addr] = true
		} else {
			ca.shortNtl += ntl
			ca.shortWallets[pr.addr] = true
		}
		if pr.leverage != nil && pr.posValue != nil {
			ca.levNum += *pr.leverage * ntl
			ca.levDen += ntl
			ca.hasLev = true
		}
	}

	for _, coin := range coinOrder {
		ca := coinMap[coin]
		row := ExposureRow{
			Coin:         coin,
			LongNtl:      ca.longNtl,
			ShortNtl:     ca.shortNtl,
			NetNtl:       ca.longNtl - ca.shortNtl,
			GrossNtl:     ca.longNtl + ca.shortNtl,
			LongWallets:  len(ca.longWallets),
			ShortWallets: len(ca.shortWallets),
		}
		if ca.hasLev && ca.levDen > 0 {
			v := ca.levNum / ca.levDen
			row.AvgLeverage = &v
		}
		resp.Rows = append(resp.Rows, row)
	}

	// freshness
	missing := 0
	membersWithData := 0
	allReady := true
	var newest time.Time
	hasTS := false
	anyStale := false
	for _, wl := range wallets {
		k := wl
		if !walletHasPos[k] {
			// could still be synced with zero positions; check sync
		}
		hasData := walletHasPos[k] || syncStatus[k] != ""
		if !hasData {
			missing++
			continue
		}
		membersWithData++
		last := syncLast[k]
		ready := syncStatus[k] == "ready"
		if last != nil {
			if time.Since(*last) > 15*time.Minute {
				ready = false
			}
			if !hasTS || last.After(newest) {
				newest = *last
				hasTS = true
			}
		} else {
			ready = false
		}
		if syncStatus[k] != "ready" {
			ready = false
		}
		if !ready {
			allReady = false
		}
		if last != nil && time.Since(*last) > 24*time.Hour {
			anyStale = true
		}
	}
	if membersWithData > 0 {
		if !hasTS || time.Since(newest) > 24*time.Hour {
			anyStale = true
		}
		if hasTS {
			tt := newest
			resp.AsOf = &tt
		}
	}
	resp.MissingWallets = missing
	resp.Partial = missing > 0
	resp.DataStatus = DeriveGroupStatus(memberCount, membersWithData, allReady, anyStale)
	return resp, nil
}
