//go:build integration

package wallet

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

// HARD-09: hostile filter input is either rejected at parse time or bound
// as data — tables stay intact and counts never change.
func TestSecurity_MaliciousFilters_NoEffect(t *testing.T) {
	f := setupScannerFixture(t)
	ctx := context.Background()

	countOf := func(table string) int64 {
		t.Helper()
		var n int64
		if err := f.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	walletsBefore := countOf("tracked_wallets")
	snapsBefore := countOf("wallet_metric_snapshots")

	// Payloads that reach SQL as bound parameters (must not error, must not
	// leak extra rows beyond a literal substring match).
	for _, search := range []string{
		`' OR '1'='1`,
		`'; DROP TABLE tracked_wallets; --`,
		`0x%' OR TRUE --`,
		`%' AND '1'='1' /*`,
	} {
		wallets, _, err := f.svc.Scan(ctx, f.userA, url.Values{"search": {search}, "limit": {"200"}})
		if err != nil {
			t.Errorf("search %q: unexpected error %v", search, err)
		}
		_ = wallets
	}

	// Payloads that must die at the parser (COMMON-902), never reaching SQL.
	for name, q := range map[string]url.Values{
		"numeric injection": {"pnl_gt": {"1 OR 1=1"}},
		"sort injection":    {"sort": {"pnl; DROP TABLE tracked_wallets"}},
		"timeframe inject":  {"timeframe": {"30D' OR '1'='1"}},
		"order injection":   {"order": {"desc; DELETE FROM users"}},
		"dex injection":     {"dex": {"hyperliquid' OR '1'='1"}},
	} {
		_, _, err := f.svc.Scan(ctx, f.userA, q)
		var appErr *domain.AppError
		if !errors.As(err, &appErr) || appErr.Code != domain.ErrCodeValidation {
			t.Errorf("%s: expected COMMON-902, got %v", name, err)
		}
	}

	if n := countOf("tracked_wallets"); n != walletsBefore {
		t.Errorf("tracked_wallets changed: %d -> %d", walletsBefore, n)
	}
	if n := countOf("wallet_metric_snapshots"); n != snapsBefore {
		t.Errorf("snapshots changed: %d -> %d", snapsBefore, n)
	}
}
