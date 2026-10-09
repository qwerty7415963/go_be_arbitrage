package hyperliquid

// Per-wallet WS subscription types (LIVE-CONTRACT v1.2 §2, probe §0 verified).
// Upstream: wss://api.hyperliquid.xyz/ws, one message per type:
// {method:"subscribe", subscription:{type, user}} (+ aggregateByTime for fills).
const (
	WSUserFills            = "userFills"
	WSOrderUpdates         = "orderUpdates"
	WSUserFundings         = "userFundings"
	WSUserNonFundingLedger = "userNonFundingLedgerUpdates"
)

// WalletSubscriptionSet is the 4-feed set each WalletWatcher holds.
var WalletSubscriptionSet = []string{
	WSUserFills, WSOrderUpdates, WSUserFundings, WSUserNonFundingLedger,
}

// WsUserFillsMsg is the userFills channel payload: snapshot + streaming fills.
type WsUserFillsMsg struct {
	IsSnapshot *bool  `json:"isSnapshot"`
	User       string `json:"user"`
	Fills      []Fill `json:"fills"`
}

// WsUserFundingMsg is one funding payment on the userFundings channel.
type WsUserFundingMsg struct {
	Time        int64  `json:"time"`
	Coin        string `json:"coin"`
	Usdc        string `json:"usdc"`
	Szi         string `json:"szi"`
	FundingRate string `json:"fundingRate"`
}

// WsUserFundingsMsg is the userFundings channel payload.
type WsUserFundingsMsg struct {
	IsSnapshot *bool              `json:"isSnapshot"`
	User       string             `json:"user"`
	Fundings   []WsUserFundingMsg `json:"fundings"`
}

// WsOrderUpdate is one order state on the orderUpdates channel.
type WsOrderUpdate struct {
	Order           OpenOrder `json:"order"`
	Status          string    `json:"status"`
	StatusTimestamp int64     `json:"statusTimestamp"`
}

// WsLedgerMsg is the userNonFundingLedgerUpdates payload.
type WsLedgerMsg struct {
	IsSnapshot bool           `json:"isSnapshot"`
	User       string         `json:"user"`
	Updates    []LedgerUpdate `json:"updates"`
}

// SubscriptionMessage builds one upstream subscribe frame for addr.
func SubscriptionMessage(subType, addr string) map[string]any {
	sub := map[string]any{"type": subType, "user": addr}
	if subType == WSUserFills {
		sub["aggregateByTime"] = false
	}
	return map[string]any{"method": "subscribe", "subscription": sub}
}
