package trader

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

func validActivitySort(sort string) bool {
	switch sort {
	case "closed_at", "opened_at", "market", "volume", "pnl", "net_pnl",
		"duration", "entry_price", "exit_price":
		return true
	}
	return false
}

func activityQueryFingerprint(venueID uuid.UUID, addr, sort, dir, result, side string) string {
	sum := sha256.Sum256([]byte(venueID.String() + "|" + addr + "|" + sort + "|" + dir + "|" + result + "|" + side))
	return fmt.Sprintf("%x", sum[:])
}

func activityCounts(all []CompletedTradeRow) ActivityCounts {
	var c ActivityCounts
	c.Total = len(all)
	for _, r := range all {
		net := r.PnL - r.Fees
		if net > 0 {
			c.Win++
		} else if net < 0 {
			c.Loss++
		}
		switch strings.ToUpper(strings.TrimSpace(r.Side)) {
		case "LONG":
			c.Long++
		case "SHORT":
			c.Short++
		}
	}
	return c
}

func filterActivityRows(all []CompletedTradeRow, result, side string) []CompletedTradeRow {
	out := make([]CompletedTradeRow, 0, len(all))
	for _, r := range all {
		if side == "long" && !strings.EqualFold(r.Side, "LONG") {
			continue
		}
		if side == "short" && !strings.EqualFold(r.Side, "SHORT") {
			continue
		}
		net := r.PnL - r.Fees
		if result == "win" && !(net > 0) {
			continue
		}
		if result == "loss" && !(net < 0) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func activityPrimaryFloat(r CompletedTradeRow, sort string) (*float64, bool) {
	var v *float64
	switch sort {
	case "volume":
		v = &r.Volume
	case "pnl":
		v = &r.PnL
	case "net_pnl":
		n := r.PnL - r.Fees
		v = &n
	case "duration":
		d := r.ClosedAt.Sub(r.OpenedAt).Seconds()
		v = &d
	case "entry_price":
		v = r.EntryPrice
	case "exit_price":
		v = r.ExitPrice
	default:
		return nil, false
	}
	return v, true
}

func sortActivityRows(rows []CompletedTradeRow, sortKey, dir string) {
	desc := dir == "desc"
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if c := compareActivityPrimary(a, b, sortKey, desc); c != 0 {
			return c < 0
		}
		if c := compareTime(a.ClosedAt, b.ClosedAt, desc); c != 0 {
			return c < 0
		}
		if a.Market != b.Market {
			return a.Market < b.Market
		}
		return a.OpenedAt.Before(b.OpenedAt)
	})
}

// compareActivityPrimary returns -1/0/+1 in display order (dir applied;
// nulls last regardless of dir).
func compareActivityPrimary(a, b CompletedTradeRow, sortKey string, desc bool) int {
	switch sortKey {
	case "closed_at":
		return compareTime(a.ClosedAt, b.ClosedAt, desc)
	case "opened_at":
		return compareTime(a.OpenedAt, b.OpenedAt, desc)
	case "market":
		if a.Market == b.Market {
			return 0
		}
		if desc {
			if a.Market > b.Market {
				return -1
			}
			return 1
		}
		if a.Market < b.Market {
			return -1
		}
		return 1
	default:
		av, ok := activityPrimaryFloat(a, sortKey)
		if !ok {
			return 0
		}
		bv, _ := activityPrimaryFloat(b, sortKey)
		return compareFloatPtr(av, bv, desc)
	}
}

func compareTime(a, b time.Time, desc bool) int {
	if a.Equal(b) {
		return 0
	}
	if desc {
		if a.After(b) {
			return -1
		}
		return 1
	}
	if a.Before(b) {
		return -1
	}
	return 1
}

func compareFloatPtr(a, b *float64, desc bool) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return 1 // nulls last
	}
	if b == nil {
		return -1
	}
	if *a == *b {
		return 0
	}
	if desc {
		if *a > *b {
			return -1
		}
		return 1
	}
	if *a < *b {
		return -1
	}
	return 1
}

// activityCursorV2 is the HMAC-sealed keyset over (sortKey, closed_at,
// market, opened_at). S is "" for NULL sort keys.
type activityCursorV2 struct {
	FP string `json:"fp"`
	S  string `json:"s"`
	C  string `json:"c"`
	M  string `json:"m"`
	O  string `json:"o"`
}

type activityAfter struct {
	sortKey  string // "" = NULL
	closedAt time.Time
	market   string
	openedAt time.Time
}

func activitySortKeyString(sortKey string, r CompletedTradeRow) string {
	switch sortKey {
	case "closed_at":
		return r.ClosedAt.UTC().Format(time.RFC3339Nano)
	case "opened_at":
		return r.OpenedAt.UTC().Format(time.RFC3339Nano)
	case "market":
		return r.Market
	case "volume":
		return strconv.FormatFloat(r.Volume, 'g', -1, 64)
	case "pnl":
		return strconv.FormatFloat(r.PnL, 'g', -1, 64)
	case "net_pnl":
		return strconv.FormatFloat(r.PnL-r.Fees, 'g', -1, 64)
	case "duration":
		return strconv.FormatFloat(r.ClosedAt.Sub(r.OpenedAt).Seconds(), 'g', -1, 64)
	case "entry_price":
		if r.EntryPrice == nil {
			return ""
		}
		return strconv.FormatFloat(*r.EntryPrice, 'g', -1, 64)
	case "exit_price":
		if r.ExitPrice == nil {
			return ""
		}
		return strconv.FormatFloat(*r.ExitPrice, 'g', -1, 64)
	}
	return ""
}

func encodeActivityCursorV2(secret []byte, fp, sortKey string, r CompletedTradeRow) (string, error) {
	raw, err := json.Marshal(activityCursorV2{
		FP: fp, S: activitySortKeyString(sortKey, r),
		C: r.ClosedAt.UTC().Format(time.RFC3339Nano),
		M: r.Market, O: r.OpenedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func decodeActivityCursorV2(secret []byte, fp, raw string) (*activityAfter, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return nil, invalidFilter("malformed cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, invalidFilter("malformed cursor")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, invalidFilter("malformed cursor")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return nil, invalidFilter("invalid cursor signature")
	}
	var c activityCursorV2
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, invalidFilter("malformed cursor")
	}
	if c.FP != fp {
		return nil, invalidFilter("cursor does not match this query")
	}
	if c.M == "" {
		return nil, invalidFilter("malformed cursor")
	}
	ct, err := time.Parse(time.RFC3339Nano, c.C)
	if err != nil {
		return nil, invalidFilter("malformed cursor")
	}
	ot, err := time.Parse(time.RFC3339Nano, c.O)
	if err != nil {
		return nil, invalidFilter("malformed cursor")
	}
	return &activityAfter{sortKey: c.S, closedAt: ct.UTC(), market: c.M, openedAt: ot.UTC()}, nil
}

// applyActivityCursor keeps rows strictly after the cursor in display order.
func applyActivityCursor(rows []CompletedTradeRow, sortKey, dir string, after *activityAfter) []CompletedTradeRow {
	if after == nil {
		return rows
	}
	desc := dir == "desc"
	out := make([]CompletedTradeRow, 0, len(rows))
	for _, r := range rows {
		if activityRowAfter(r, sortKey, desc, after) {
			out = append(out, r)
		}
	}
	return out
}

func activityRowAfter(r CompletedTradeRow, sortKey string, desc bool, after *activityAfter) bool {
	c := compareRowToAfter(r, sortKey, desc, after)
	return c > 0
}

// compareRowToAfter returns >0 when r comes after the cursor in display order.
func compareRowToAfter(r CompletedTradeRow, sortKey string, desc bool, after *activityAfter) int {
	if c := comparePrimaryToAfter(r, sortKey, desc, after.sortKey); c != 0 {
		return c
	}
	if c := compareTimeToAfter(r.ClosedAt, after.closedAt, desc, sortKey == "closed_at"); c != 0 {
		return c
	}
	if r.Market != after.market {
		// Market tiebreak always ASC for stability (matches legacy).
		if r.Market > after.market {
			return 1
		}
		return -1
	}
	if r.OpenedAt.Equal(after.openedAt) {
		return 0
	}
	if r.OpenedAt.After(after.openedAt) {
		return 1
	}
	return -1
}

func compareTimeToAfter(t, after time.Time, desc, isPrimary bool) int {
	if t.Equal(after) {
		if isPrimary {
			return 0
		}
		return 0
	}
	if desc {
		if t.Before(after) {
			return 1
		}
		return -1
	}
	if t.After(after) {
		return 1
	}
	return -1
}

func comparePrimaryToAfter(r CompletedTradeRow, sortKey string, desc bool, afterS string) int {
	switch sortKey {
	case "closed_at":
		afterT, err := time.Parse(time.RFC3339Nano, afterS)
		if err != nil {
			return 0
		}
		return compareTimeToAfter(r.ClosedAt, afterT.UTC(), desc, true)
	case "opened_at":
		afterT, err := time.Parse(time.RFC3339Nano, afterS)
		if err != nil {
			return 0
		}
		return compareTimeToAfter(r.OpenedAt, afterT.UTC(), desc, true)
	case "market":
		if r.Market == afterS {
			return 0
		}
		afterGreater := afterS > r.Market
		rowGreater := r.Market > afterS
		if desc {
			// desc: display order large→small; after means smaller.
			if rowGreater {
				return -1
			}
			if afterGreater {
				return 1
			}
			return 0
		}
		if rowGreater {
			return 1
		}
		return -1
	default:
		rv, ok := activityPrimaryFloat(r, sortKey)
		if !ok {
			return 0
		}
		if afterS == "" {
			// Cursor NULL (last row null): nothing after nulls (nulls last).
			// Row NULL vs NULL cursor: tie → fall through to tiebreakers.
			if rv == nil {
				return 0
			}
			// Row non-null vs NULL cursor: row comes before nulls, not after.
			return -1
		}
		if rv == nil {
			// Row NULL, cursor non-null: nulls last → row after.
			return 1
		}
		afterF, err := strconv.ParseFloat(afterS, 64)
		if err != nil {
			return 0
		}
		if *rv == afterF {
			return 0
		}
		if desc {
			if *rv < afterF {
				return 1
			}
			return -1
		}
		if *rv > afterF {
			return 1
		}
		return -1
	}
}
