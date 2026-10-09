package hyperliquid

import (
	"context"
	"time"
)

// FundingDelta is the delta payload of a userFunding ledger update.
// Shape verified against Hyperliquid docs (info/userFunding):
// {type:"funding", coin, usdc (signed, negative = paid),
// szi, fundingRate, nSamples (nullable)}.
type FundingDelta struct {
	Type        string `json:"type"`
	Coin        string `json:"coin"`
	Usdc        string `json:"usdc"`
	Szi         string `json:"szi"`
	FundingRate string `json:"fundingRate"`
	NSamples    *int   `json:"nSamples"`
}

// FundingUpdate is one row of userFunding history.
type FundingUpdate struct {
	Time  int64        `json:"time"`
	Hash  string       `json:"hash"`
	Delta FundingDelta `json:"delta"`
}

// FundingPayment is the normalized funding payment used for attribution:
// signed USDC amount (negative = paid by the wallet) at Time for Coin.
type FundingPayment struct {
	Time time.Time
	Coin string
	Usdc float64
}

// FetchUserFunding returns funding payments for address in
// [startMs, endMs] (endMs <= 0 means venue default = now).
// Public: POST {type:"userFunding", user, startTime, endTime?} to /info.
// Paced via the shared AdaptivePacer like FetchWindow.
func (c *Client) FetchUserFunding(ctx context.Context, address string, startMs, endMs int64) ([]FundingUpdate, error) {
	payload := map[string]any{
		"type":      "userFunding",
		"user":      address,
		"startTime": startMs,
	}
	if endMs > 0 {
		payload["endTime"] = endMs
	}
	var out []FundingUpdate
	if err := c.postInfo(ctx, payload, &out, "userFunding", 32<<20); err != nil {
		return nil, err
	}
	if out == nil {
		out = []FundingUpdate{}
	}
	return out, nil
}
