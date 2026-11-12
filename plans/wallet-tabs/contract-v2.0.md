# Contract v2.0 — Trader Overview

Endpoint: GET /api/v1/traders/overview
Auth: Public (no JWT), same as /traders/search.

Query params:
- venue (string, default "hyperliquid")
- period (enum 1D/7D/30D/ALL, default 30D)
- pnl_metric (enum "pnl"|"realized_pnl", default "realized_pnl")
- series_days (int, 7..90, default 30)

Response 200:
{
  "success": true,
  "data": {
    "as_of": "RFC3339",
    "venue": "string",
    "period": "string",
    "pnl_metric": "string",
    "universe_size": int,
    "pnl_buckets": [
      {
        "id": "extremely_profitable|very_profitable|profitable|unprofitable|very_unprofitable|rekt",
        "min_pnl": number|null,
        "max_pnl": number|null,
        "wallet_count": int,
        "pnl_total": number,
        "volume_total": number,
        "long_count": int,
        "short_count": int,
        "long_ratio": number,
        "long_notional": number,
        "short_notional": number,
        "sentiment": "very_bullish|bullish|slightly_bullish|neutral|slightly_bearish|bearish|very_bearish",
        "series": [{"t":"RFC3339-date","v":number}]
      }
    ],
    "account_buckets": [
      {
        "id": "apex|whale|large|medium|small",
        "min_equity": number|null,
        "max_equity": number|null,
        "wallet_count": int,
        "equity_total": number,
        "volume_total": number,
        "long_count": int,
        "short_count": int,
        "long_ratio": number,
         "long_notional": number,
         "short_notional": number,
         "sentiment": "very_bullish|bullish|slightly_bullish|neutral|slightly_bearish|bearish|very_bearish",
         "series": [{"t":"RFC3339-date","v":number}]
       }
     ],
     "coverage": {
      "pnl_wallets": int,
      "account_value_wallets": int,
      "series_wallets": int,
      "data_status": {"ready":int,"stale":int,"error":int,"syncing":int}
    }
  }
}

Errors: 400 invalid params, 500 internal.

Bucket definitions:
PnL (on pnl_metric value):
- extremely_profitable: >= 1,000,000
- very_profitable: 100,000 .. 1,000,000 (exclusive upper)
- profitable: 0 < pnl <= 100,000
- unprofitable: -100,000 <= pnl < 0
- very_unprofitable: -1,000,000 .. -100,000
- rekt: <= -1,000,000

Account (on latest end_equity from trader_equity_daily):
- apex: >= 5,000,000
- whale: 1,000,000 .. 5,000,000
- large: 100,000 .. 1,000,000
- medium: 10,000 .. 100,000
- small: 0 .. 10,000 (equity <= 0 or null excluded)

Sentiment (from long_ratio = long_count/(long_count+short_count)):
- >=0.65 very_bullish, 0.55-0.65 bullish, 0.52-0.55 slightly_bullish, 0.48-0.52 neutral, 0.45-0.48 slightly_bearish, 0.35-0.45 bearish, <0.35 very_bearish
- if total counts 0 -> neutral

Long/short notional (approx): long_notional = volume * long_count/(long_count+short_count); short_notional = volume * short_count/(...). If total counts 0 -> 0.

Series: cumulative realized_pnl per bucket per day over series_days, from trader_daily_stats joined to bucket mapping. Missing days carry forward last cumulative value. Use 0 for null realized_pnl.

Data sources:
- PnL buckets: trader_period_metrics (venue_id, period) filtered by pnl_metric IS NOT NULL AND pnl_metric <> 0.
- Account buckets: latest trader_equity_daily.end_equity per wallet (DISTINCT ON wallet_address ORDER BY stat_date DESC), joined to trader_period_metrics for counts (LEFT JOIN).
- Series: trader_daily_stats joined to trader_period_metrics bucket mapping, grouped by stat_date + bucket, SUM(realized_pnl).
- Coverage: counts of wallets with metrics (including zero-PnL), with equity, with series; data_status distribution from trader_period_metrics (all wallets).

Cache: in-memory, TTL 5 min, key venue|period|series_days, single-flight (use sync.Mutex). No DB schema changes.

Auth: public (no JWT), same as /traders/search.
