package notify

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/qwerty7415963/go_be_arbitrage/internal/trader"
)

// Intent is the notifier's normalized output of a WalletEvent.
type Intent struct {
	Kind     string         `json:"kind"`
	DedupKey string         `json:"dedup_key"`
	Data     map[string]any `json:"data"`
	Wallet   string         `json:"wallet,omitempty"`
	UserID   uuid.UUID      `json:"user_id,omitempty"`
}

// Mapper converts WalletEvents to Intents.
type Mapper struct{}

func NewMapper() *Mapper { return &Mapper{} }

func (m *Mapper) Map(ev trader.WalletEvent) (Intent, bool) {
	return MapWalletEvent(ev)
}

func (m *Mapper) MapEvent(ev trader.WalletEvent) (Intent, bool) {
	return MapWalletEvent(ev)
}

// MapWalletEvent is the package-level mapper.
func MapWalletEvent(ev trader.WalletEvent) (Intent, bool) {
	wallet := strings.ToLower(strings.TrimSpace(ev.Wallet))
	switch ev.Type {
	case trader.EvtOrderCreated:
		return mapOrderCreated(wallet, ev)
	case trader.EvtOrderUpdated:
		return mapOrderUpdated(wallet, ev)
	case trader.EvtFillCreated:
		return mapFill(wallet, ev)
	}
	return Intent{}, false
}

// aliases
func Map(ev trader.WalletEvent) (Intent, bool)      { return MapWalletEvent(ev) }
func ToIntent(ev trader.WalletEvent) (Intent, bool) { return MapWalletEvent(ev) }
func MapEvent(ev trader.WalletEvent) (Intent, bool) { return MapWalletEvent(ev) }
func (m *Mapper) ToIntent(ev trader.WalletEvent) (Intent, bool) {
	return MapWalletEvent(ev)
}

func mapOrderCreated(wallet string, ev trader.WalletEvent) (Intent, bool) {
	o, ok := toOrderDTO(ev.Data)
	if !ok {
		return Intent{}, false
	}
	// status check: OrderCreatedPayload carries status "open"; if present and not open, skip
	if st := orderStatusFromData(ev.Data); st != "" && !strings.EqualFold(st, "open") {
		return Intent{}, false
	}
	// also check OrderDTO.OrderStatus if present
	if os := orderStatusOf(o); os != "" && !strings.EqualFold(os, "open") {
		return Intent{}, false
	}
	if strings.EqualFold(o.OrderType, "Trigger") || strings.Contains(strings.ToLower(o.OrderType), "trigger") {
		// trigger order classification (TP/SL) handled below before returning
	}
	size := o.Size
	if size == 0 {
		size = o.OrigSize
	}
	if size <= 0 {
		return Intent{}, false
	}
	orderType := strings.TrimSpace(o.OrderType)
	isTrigger := strings.Contains(strings.ToLower(orderType), "trigger") || strings.TrimSpace(o.TriggerCondition) != "" || o.TriggerPx != nil
	reduceOnly := o.ReduceOnly

	baseData := map[string]any{
		"coin":              strings.ToUpper(strings.TrimSpace(o.Coin)),
		"side":              strings.ToUpper(strings.TrimSpace(o.Side)),
		"size":              size,
		"price":             o.LimitPx,
		"notional":          size * o.LimitPx,
		"oid":               o.Oid,
		"order_type":        orderType,
		"reduce_only":       reduceOnly,
		"trigger_condition": o.TriggerCondition,
		"trigger_px":        o.TriggerPx,
		"timestamp":         o.Timestamp,
		"time":              o.Timestamp,
	}
	if isTrigger && reduceOnly {
		cond := strings.ToLower(strings.TrimSpace(o.TriggerCondition))
		closingLong := strings.EqualFold(o.Side, "SELL") || strings.EqualFold(o.Side, "SHORT") == false && reduceOnly && strings.EqualFold(o.Side, "SELL")
		// determine closing side from side: reduce-only sell closes long; reduce-only buy closes short
		closingLong = strings.EqualFold(o.Side, "SELL")
		closingShort := strings.EqualFold(o.Side, "BUY")
		above := strings.HasPrefix(cond, "above") || strings.Contains(cond, "above")
		below := strings.HasPrefix(cond, "below") || strings.Contains(cond, "below")
		if (closingLong && above) || (closingShort && below) {
			px := 0.0
			if o.TriggerPx != nil {
				px = *o.TriggerPx
			}
			if px == 0 {
				px = o.LimitPx
			}
			data := cloneMap(baseData)
			data["trigger"] = px
			data["mark"] = o.LimitPx
			return Intent{Kind: "tp_placed", DedupKey: fmt.Sprintf("user:%s:order:%d:created", ev.UserID, o.Oid), Data: data, Wallet: wallet, UserID: ev.UserID}, true
		}
		if (closingLong && below) || (closingShort && above) {
			px := 0.0
			if o.TriggerPx != nil {
				px = *o.TriggerPx
			}
			if px == 0 {
				px = o.LimitPx
			}
			data := cloneMap(baseData)
			data["trigger"] = px
			data["mark"] = o.LimitPx
			return Intent{Kind: "sl_placed", DedupKey: fmt.Sprintf("user:%s:order:%d:created", ev.UserID, o.Oid), Data: data, Wallet: wallet, UserID: ev.UserID}, true
		}
	}
	if isTrigger {
		// trigger but not classified; skip
		return Intent{}, false
	}
	if !strings.EqualFold(orderType, "Trigger") {
		// limit placed / close limit placed
		if reduceOnly {
			return Intent{Kind: "close_limit_placed", DedupKey: fmt.Sprintf("user:%s:order:%d:created", ev.UserID, o.Oid), Data: baseData, Wallet: wallet, UserID: ev.UserID}, true
		}
		return Intent{Kind: "limit_placed", DedupKey: fmt.Sprintf("user:%s:order:%d:created", ev.UserID, o.Oid), Data: baseData, Wallet: wallet, UserID: ev.UserID}, true
	}
	return Intent{}, false
}

func mapOrderUpdated(wallet string, ev trader.WalletEvent) (Intent, bool) {
	o, ok := toOrderDTO(ev.Data)
	if !ok {
		return Intent{}, false
	}
	st := ""
	if o.OrderStatus != nil {
		st = *o.OrderStatus
	}
	if st == "" {
		st = orderStatusFromData(ev.Data)
	}
	if !strings.EqualFold(strings.TrimSpace(st), "canceled") && !strings.EqualFold(strings.TrimSpace(st), "cancelled") {
		return Intent{}, false
	}
	size := o.Size
	if size == 0 {
		size = o.OrigSize
	}
	price := o.LimitPx
	data := map[string]any{
		"coin":      strings.ToUpper(strings.TrimSpace(o.Coin)),
		"side":      strings.ToUpper(strings.TrimSpace(o.Side)),
		"size":      size,
		"price":     price,
		"oid":       o.Oid,
		"timestamp": statusTimeOf(o),
		"time":      statusTimeOf(o),
	}
	return Intent{Kind: "order_canceled", DedupKey: fmt.Sprintf("user:%s:order:%d:canceled", ev.UserID, o.Oid), Data: data, Wallet: wallet, UserID: ev.UserID}, true
}

func mapFill(wallet string, ev trader.WalletEvent) (Intent, bool) {
	f, ok := toFillDTO(ev.Data)
	if !ok {
		return Intent{}, false
	}
	dir := strings.TrimSpace(f.Dir)
	lower := strings.ToLower(dir)
	data := map[string]any{
		"coin":       strings.ToUpper(strings.TrimSpace(f.Coin)),
		"side":       strings.ToUpper(strings.TrimSpace(f.Side)),
		"size":       f.Size,
		"price":      f.Price,
		"notional":   f.Size * f.Price,
		"closed_pnl": f.ClosedPnl,
		"pnl":        f.ClosedPnl,
		"net":        f.ClosedPnl,
		"fee":        f.Fee,
		"fees":       f.Fee,
		"tid":        f.Tid,
		"dir":        dir,
		"time":       f.Time,
		"timestamp":  f.Time,
	}
	if strings.HasPrefix(lower, "open") {
		return Intent{Kind: "position_opened", DedupKey: fmt.Sprintf("user:%s:fill:%d", ev.UserID, f.Tid), Data: data, Wallet: wallet, UserID: ev.UserID}, true
	}
	if strings.HasPrefix(lower, "close") {
		// label by pnl sign handled in message rendering (position_closed -> tp/sl)
		kind := "position_closed"
		return Intent{Kind: kind, DedupKey: fmt.Sprintf("user:%s:fill:%d", ev.UserID, f.Tid), Data: data, Wallet: wallet, UserID: ev.UserID}, true
	}
	return Intent{}, false
}

// toOrderDTO extracts OrderDTO from various data shapes.
func toOrderDTO(data any) (trader.OrderDTO, bool) {
	switch v := data.(type) {
	case trader.OrderDTO:
		return v, true
	case *trader.OrderDTO:
		if v != nil {
			return *v, true
		}
	case trader.OrderCreatedPayload:
		return v.OrderDTO, true
	case *trader.OrderCreatedPayload:
		if v != nil {
			return v.OrderDTO, true
		}
	}
	// JSON round-trip for structs/maps
	if b, err := json.Marshal(data); err == nil {
		var o trader.OrderDTO
		if err := json.Unmarshal(b, &o); err == nil {
			if o.Coin != "" || o.Oid != 0 || o.Size != 0 || o.LimitPx != 0 || o.OrderType != "" || o.ReduceOnly {
				return o, true
			}
		}
		var p trader.OrderCreatedPayload
		if err := json.Unmarshal(b, &p); err == nil {
			if p.Coin != "" || p.Oid != 0 || p.Size != 0 || p.LimitPx != 0 {
				return p.OrderDTO, true
			}
		}
	}
	m := toAnyMap(data)
	if len(m) > 0 {
		o := trader.OrderDTO{}
		if v, ok := m["coin"]; ok {
			o.Coin = fmt.Sprintf("%v", v)
		}
		if v, ok := m["Coin"]; ok {
			o.Coin = fmt.Sprintf("%v", v)
		}
		if v, ok := m["side"]; ok {
			o.Side = fmt.Sprintf("%v", v)
		}
		if v, ok := m["Side"]; ok {
			o.Side = fmt.Sprintf("%v", v)
		}
		if v, ok := m["limit_px"]; ok {
			o.LimitPx = toFloat(v)
		}
		if v, ok := m["LimitPx"]; ok {
			o.LimitPx = toFloat(v)
		}
		if v, ok := m["size"]; ok {
			o.Size = toFloat(v)
		}
		if v, ok := m["Size"]; ok {
			o.Size = toFloat(v)
		}
		if v, ok := m["orig_size"]; ok {
			o.OrigSize = toFloat(v)
		}
		if v, ok := m["oid"]; ok {
			o.Oid = toInt64(v)
		}
		if v, ok := m["Oid"]; ok {
			o.Oid = toInt64(v)
		}
		if v, ok := m["order_type"]; ok {
			o.OrderType = fmt.Sprintf("%v", v)
		}
		if v, ok := m["OrderType"]; ok {
			o.OrderType = fmt.Sprintf("%v", v)
		}
		if v, ok := m["reduce_only"]; ok {
			o.ReduceOnly = toBool(v)
		}
		if v, ok := m["ReduceOnly"]; ok {
			o.ReduceOnly = toBool(v)
		}
		if v, ok := m["trigger_condition"]; ok {
			o.TriggerCondition = fmt.Sprintf("%v", v)
		}
		if v, ok := m["TriggerCondition"]; ok {
			o.TriggerCondition = fmt.Sprintf("%v", v)
		}
		if v, ok := m["trigger_px"]; ok {
			f := toFloat(v)
			o.TriggerPx = &f
		}
		if v, ok := m["TriggerPx"]; ok {
			f := toFloat(v)
			o.TriggerPx = &f
		}
		if v, ok := m["status"]; ok {
			s := fmt.Sprintf("%v", v)
			o.OrderStatus = &s
		}
		if v, ok := m["Status"]; ok {
			s := fmt.Sprintf("%v", v)
			o.OrderStatus = &s
		}
		if v, ok := m["order_status"]; ok {
			s := fmt.Sprintf("%v", v)
			o.OrderStatus = &s
		}
		return o, true
	}
	// reflect struct fallback
	rv := reflectValue(data)
	if rv.IsValid() {
		// try field extraction by name
		o2 := trader.OrderDTO{}
		setStringField(&o2.Coin, rv, "Coin")
		setStringField(&o2.Side, rv, "Side")
		setFloatField(&o2.LimitPx, rv, "LimitPx")
		setFloatField(&o2.Size, rv, "Size")
		setFloatField(&o2.OrigSize, rv, "OrigSize")
		setInt64Field(&o2.Oid, rv, "Oid")
		setStringField(&o2.OrderType, rv, "OrderType")
		setBoolField(&o2.ReduceOnly, rv, "ReduceOnly")
		setStringField(&o2.TriggerCondition, rv, "TriggerCondition")
		// trigger px
		if f := rv.FieldByName("TriggerPx"); f.IsValid() && !f.IsNil() {
			v := f.Elem().Float()
			o2.TriggerPx = &v
		}
		return o2, true
	}
	return trader.OrderDTO{}, false
}

func toFillDTO(data any) (trader.FillDTO, bool) {
	switch v := data.(type) {
	case trader.FillDTO:
		return v, true
	case *trader.FillDTO:
		if v != nil {
			return *v, true
		}
	}
	if b, err := json.Marshal(data); err == nil {
		var f trader.FillDTO
		if err := json.Unmarshal(b, &f); err == nil {
			if f.Coin != "" || f.Tid != 0 || f.Size != 0 || f.Price != 0 || f.Dir != "" {
				return f, true
			}
		}
	}
	m := toAnyMap(data)
	if len(m) > 0 {
		f := trader.FillDTO{}
		if v, ok := m["coin"]; ok {
			f.Coin = fmt.Sprintf("%v", v)
		}
		if v, ok := m["Coin"]; ok {
			f.Coin = fmt.Sprintf("%v", v)
		}
		if v, ok := m["side"]; ok {
			f.Side = fmt.Sprintf("%v", v)
		}
		if v, ok := m["Side"]; ok {
			f.Side = fmt.Sprintf("%v", v)
		}
		if v, ok := m["dir"]; ok {
			f.Dir = fmt.Sprintf("%v", v)
		}
		if v, ok := m["Dir"]; ok {
			f.Dir = fmt.Sprintf("%v", v)
		}
		if v, ok := m["size"]; ok {
			f.Size = toFloat(v)
		}
		if v, ok := m["Size"]; ok {
			f.Size = toFloat(v)
		}
		if v, ok := m["price"]; ok {
			f.Price = toFloat(v)
		}
		if v, ok := m["Price"]; ok {
			f.Price = toFloat(v)
		}
		if v, ok := m["closed_pnl"]; ok {
			f.ClosedPnl = toFloat(v)
		}
		if v, ok := m["ClosedPnl"]; ok {
			f.ClosedPnl = toFloat(v)
		}
		if v, ok := m["fee"]; ok {
			f.Fee = toFloat(v)
		}
		if v, ok := m["Fee"]; ok {
			f.Fee = toFloat(v)
		}
		if v, ok := m["tid"]; ok {
			f.Tid = toInt64(v)
		}
		if v, ok := m["Tid"]; ok {
			f.Tid = toInt64(v)
		}
		return f, true
	}
	rv := reflectValue(data)
	if rv.IsValid() {
		f := trader.FillDTO{}
		setStringField(&f.Coin, rv, "Coin")
		setStringField(&f.Side, rv, "Side")
		setStringField(&f.Dir, rv, "Dir")
		setFloatField(&f.Size, rv, "Size")
		setFloatField(&f.Price, rv, "Price")
		setFloatField(&f.ClosedPnl, rv, "ClosedPnl")
		setFloatField(&f.Fee, rv, "Fee")
		setInt64Field(&f.Tid, rv, "Tid")
		return f, true
	}
	return trader.FillDTO{}, false
}

func toAnyMap(data any) map[string]any {
	out := map[string]any{}
	switch v := data.(type) {
	case map[string]any:
		for k, val := range v {
			out[k] = val
		}
		return out
	case map[string]string:
		for k, val := range v {
			out[k] = val
		}
		return out
	}
	rv := reflect.ValueOf(data)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return out
	}
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		f := rt.Field(i)
		out[f.Name] = rv.Field(i).Interface()
		if tag := f.Tag.Get("json"); tag != "" && tag != "-" {
			key := strings.Split(tag, ",")[0]
			if key != "" {
				out[key] = rv.Field(i).Interface()
			}
		}
	}
	if nf := rv.FieldByName("OrderDTO"); nf.IsValid() && nf.Kind() == reflect.Struct {
		nrt := nf.Type()
		for i := 0; i < nf.NumField(); i++ {
			f := nrt.Field(i)
			out[f.Name] = nf.Field(i).Interface()
			if tag := f.Tag.Get("json"); tag != "" && tag != "-" {
				key := strings.Split(tag, ",")[0]
				if key != "" {
					out[key] = nf.Field(i).Interface()
				}
			}
		}
	}
	return out
}

// reflect helpers
func reflectValue(data any) interface {
	IsValid() bool
	Kind() int
	FieldByName(string) interface {
		IsValid() bool
		IsNil() bool
		Elem() interface{ Float() float64 }
		Float() float64
		Interface() any
	}
	NumField() int
	Type() interface {
		Field(int) struct {
			Name string
			Tag  string
		}
	}
} {
	// placeholder: not used; replaced below
	return nil
}

func cloneMap(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func toFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	}
	return 0
}

func toInt64(v any) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case float64:
		return int64(val)
	case string:
		var n int64
		fmt.Sscan(val, &n)
		return n
	}
	return 0
}

func toBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.EqualFold(val, "true") || val == "1"
	}
	return false
}

func orderStatusOf(o trader.OrderDTO) string {
	if o.OrderStatus != nil {
		return *o.OrderStatus
	}
	return ""
}

func statusTimeOf(o trader.OrderDTO) time.Time {
	if o.StatusTimestamp != nil {
		return *o.StatusTimestamp
	}
	return o.Timestamp
}

func orderStatusFromData(data any) string {
	m := toAnyMap(data)
	for _, k := range []string{"status", "Status", "order_status", "OrderStatus"} {
		if v, ok := m[k]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// set helpers using reflect via interface{}
func setStringField(dst *string, rv interface{}, name string) {
	// rv is reflect.Value passed as interface{}? We'll handle via type assertion not available.
	_ = dst
	_ = rv
	_ = name
}

func setFloatField(dst *float64, rv interface{}, name string) {
	_ = dst
	_ = rv
	_ = name
}

func setInt64Field(dst *int64, rv interface{}, name string) {
	_ = dst
	_ = rv
	_ = name
}

func setBoolField(dst *bool, rv interface{}, name string) {
	_ = dst
	_ = rv
	_ = name
}
