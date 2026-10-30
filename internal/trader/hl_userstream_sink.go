package trader

import (
	"strings"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/hyperliquid"
)

// HyperliquidUserStreamSink adapts a WatcherManager to hyperliquid.UserStream's
// event sink interface (hyperliquid types, to avoid an import cycle).
type HyperliquidUserStreamSink struct {
	Mgr *WatcherManager
}

func (s *HyperliquidUserStreamSink) OnRawUserFill(addr string, fill hyperliquid.Fill) {
	if s == nil || s.Mgr == nil {
		return
	}
	s.Mgr.OnRawUserFill(addr, fill)
}

func (s *HyperliquidUserStreamSink) OnOrder(addr string, upd hyperliquid.WsOrderUpdate) {
	if s == nil || s.Mgr == nil {
		return
	}
	dtos := mapOpenOrders([]hyperliquid.OpenOrder{upd.Order}, false)
	if len(dtos) == 0 {
		dto := OrderDTO{
			Coin: strings.ToUpper(strings.TrimSpace(upd.Order.Coin)),
			Side: normalizeSideForOrder(upd.Order.Side),
			Oid:  upd.Order.Oid,
		}
		s.Mgr.OnOrder(addr, dto)
		return
	}
	dto := dtos[0]
	if upd.Status != "" {
		st := upd.Status
		dto.OrderStatus = &st
	}
	if upd.StatusTimestamp > 0 {
		t := time.UnixMilli(upd.StatusTimestamp).UTC()
		dto.StatusTimestamp = &t
	}
	s.Mgr.OnOrder(addr, dto)
}
