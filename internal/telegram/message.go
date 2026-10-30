package telegram

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
)

// Message kinds.
const (
	MsgLimitPlaced      = "limit_placed"
	MsgCloseLimitPlaced = "close_limit_placed"
	MsgOrderCanceled    = "order_canceled"
	MsgPositionOpened   = "position_opened"
	MsgPositionClosed   = "position_closed"
	MsgTakeProfit       = "take_profit"
	MsgStopLoss         = "stop_loss"
	MsgTPPlaced         = "tp_placed"
	MsgSLPlaced         = "sl_placed"
)

var placeholderRe = regexp.MustCompile(`\{[^}]+\}`)

var templates = map[string]map[Locale]string{
	MsgLimitPlaced: {
		LocaleVI: "🟢 <b>ĐẶT LỆNH LIMIT</b> tại <i>{group}</i>\n{who}\n{COIN} {SIDE} đặt {size} @ ${price}\nGiá trị ≈ ${notional} lúc {HH:MM DD/MM}\n<a href=\"{link}\">Xem ví →</a>",
		LocaleEN: "🟢 <b>LIMIT ORDER PLACED</b> in <i>{group}</i>\n{who}\n{COIN} {SIDE} placed {size} @ ${price}\nValue ≈ ${notional} at {HH:MM DD/MM}\n<a href=\"{link}\">View wallet →</a>",
	},
	MsgCloseLimitPlaced: {
		LocaleVI: "🟢 <b>ĐẶT LỆNH ĐÓNG</b> tại <i>{group}</i>\n{who}\n{COIN} {SIDE} đóng {size} @ ${price}\nGiá trị ≈ ${notional} lúc {HH:MM DD/MM}\n<a href=\"{link}\">Xem ví →</a>",
		LocaleEN: "🟢 <b>CLOSE ORDER PLACED</b> in <i>{group}</i>\n{who}\n{COIN} {SIDE} closing {size} @ ${price}\nValue ≈ ${notional} at {HH:MM DD/MM}\n<a href=\"{link}\">View wallet →</a>",
	},
	MsgOrderCanceled: {
		LocaleVI: "🔴 <b>HỦY LỆNH</b> tại <i>{group}</i>\n{who}\n{COIN} lệnh {size} @ ${price} đã hủy\n{HH:MM DD/MM} <a href=\"{link}\">Xem ví →</a>",
		LocaleEN: "🔴 <b>ORDER CANCELED</b> in <i>{group}</i>\n{who}\n{COIN} order {size} @ ${price} canceled\n{HH:MM DD/MM} <a href=\"{link}\">View wallet →</a>",
	},
	MsgPositionOpened: {
		LocaleVI: "🔵 <b>MỞ VỊ THẾ</b> tại <i>{group}</i>\n{who}\n{COIN} {SIDE} mở {size} @ ${price}\nGiá trị ≈ ${notional} lúc {HH:MM DD/MM}\n<a href=\"{link}\">Xem ví →</a>",
		LocaleEN: "🔵 <b>POSITION OPENED</b> in <i>{group}</i>\n{who}\n{COIN} {SIDE} opened {size} @ ${price}\nValue ≈ ${notional} at {HH:MM DD/MM}\n<a href=\"{link}\">View wallet →</a>",
	},
	MsgTakeProfit: {
		LocaleVI: "✅ <b>CHỐT LỜI</b> tại <i>{group}</i>\n{who}\n{COIN} {SIDE} {size}\nVào ${entry} → Ra ${exit}\nPnL ròng <b>+${net}</b> (phí ${fees})\nGiờ {duration} <a href=\"{link}\">Xem ví →</a>",
		LocaleEN: "✅ <b>TAKE PROFIT</b> in <i>{group}</i>\n{who}\n{COIN} {SIDE} {size}\nEntry ${entry} → Exit ${exit}\nNet PnL <b>+${net}</b> (fees ${fees})\nDuration {duration} <a href=\"{link}\">View wallet →</a>",
	},
	MsgStopLoss: {
		LocaleVI: "❌ <b>CẮT LỖ</b> tại <i>{group}</i>\n{who}\n{COIN} {SIDE} {size}\nVào ${entry} → Ra ${exit}\nPnL ròng <b>-${net}</b> (phí ${fees})\nGiờ {duration} <a href=\"{link}\">Xem ví →</a>",
		LocaleEN: "❌ <b>STOP LOSS</b> in <i>{group}</i>\n{who}\n{COIN} {SIDE} {size}\nEntry ${entry} → Exit ${exit}\nNet PnL <b>-${net}</b> (fees ${fees})\nDuration {duration} <a href=\"{link}\">View wallet →</a>",
	},
	MsgTPPlaced: {
		LocaleVI: "🎯 <b>ĐẶT CHỐT LỜI (TP)</b> tại <i>{group}</i>\n{who}\n{COIN} {SIDE} đặt TP ${trigger} (mark ${mark})",
		LocaleEN: "🎯 <b>TAKE PROFIT SET</b> in <i>{group}</i>\n{who}\n{COIN} {SIDE} TP ${trigger} (mark ${mark})",
	},
	MsgSLPlaced: {
		LocaleVI: "🛡️ <b>ĐẶT CẮT LỖ (SL)</b> tại <i>{group}</i>\n{who}\n{COIN} {SIDE} đặt SL ${trigger} (mark ${mark})",
		LocaleEN: "🛡️ <b>STOP LOSS SET</b> in <i>{group}</i>\n{who}\n{COIN} {SIDE} SL ${trigger} (mark ${mark})",
	},
}

func normalizeKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch k {
	case "limitplaced", "limit_placed", "limit placed":
		return MsgLimitPlaced
	case "closelimitplaced", "close_limit_placed", "close order placed", "close_limit":
		return MsgCloseLimitPlaced
	case "ordercanceled", "order_canceled", "order_cancelled", "canceled":
		return MsgOrderCanceled
	case "positionopened", "position_opened", "opened":
		return MsgPositionOpened
	case "positionclosed", "position_closed", "closed":
		return MsgPositionClosed
	case "takeprofit", "take_profit", "tp":
		return MsgTakeProfit
	case "stoploss", "stop_loss", "sl":
		return MsgStopLoss
	case "tpplaced", "tp_placed", "take_profit_set", "tp_set":
		return MsgTPPlaced
	case "slplaced", "sl_placed", "stop_loss_set", "sl_set":
		return MsgSLPlaced
	default:
		return k
	}
}

func isNegative(v string) bool {
	v = strings.TrimSpace(v)
	return strings.HasPrefix(v, "-")
}

func toMap(data interface{}) map[string]string {
	out := map[string]string{}
	if data == nil {
		return out
	}
	if m, ok := data.(map[string]string); ok {
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	if m, ok := data.(map[string]interface{}); ok {
		for k, v := range m {
			out[k] = fmt.Sprintf("%v", v)
		}
		return out
	}
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return out
	}
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		f := t.Field(i)
		name := f.Name
		val := v.Field(i)
		str := ""
		switch val.Kind() {
		case reflect.Pointer:
			if !val.IsNil() {
				str = fmt.Sprintf("%v", val.Elem().Interface())
			}
		default:
			str = fmt.Sprintf("%v", val.Interface())
		}
		out[name] = str
		out[strings.ToLower(name)] = str
		// json tag
		if tag := f.Tag.Get("json"); tag != "" && tag != "-" {
			key := strings.Split(tag, ",")[0]
			if key != "" {
				out[key] = str
			}
		}
	}
	return out
}

func lookup(data map[string]string, placeholder string) string {
	// exact
	if v, ok := data[placeholder]; ok {
		return v
	}
	// lower
	low := strings.ToLower(placeholder)
	if v, ok := data[low]; ok {
		return v
	}
	// normalized
	norm := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, placeholder)
	for k, v := range data {
		if strings.EqualFold(strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, k), norm) {
			return v
		}
	}
	// aliases
	aliasMap := map[string][]string{
		"group":    {"group", "GroupName", "groupname"},
		"who":      {"who", "Who"},
		"coin":     {"coin", "Coin", "COIN"},
		"side":     {"side", "Side", "SIDE"},
		"size":     {"size", "Size"},
		"price":    {"price", "Price"},
		"notional": {"notional", "Notional"},
		"time":     {"time", "Time", "datetime", "Datetime", "HHMMDDMM"},
		"link":     {"link", "Link"},
		"entry":    {"entry", "Entry"},
		"exit":     {"exit", "Exit"},
		"net":      {"net", "Net", "pnl", "PnL", "closedpnl"},
		"fees":     {"fees", "Fees"},
		"duration": {"duration", "Duration"},
		"trigger":  {"trigger", "Trigger"},
		"mark":     {"mark", "Mark"},
	}
	normLow := strings.ToLower(norm)
	for _, cand := range aliasMap[normLow] {
		if v, ok := data[cand]; ok {
			return v
		}
		if v, ok := data[strings.ToLower(cand)]; ok {
			return v
		}
	}
	return ""
}

func RenderMessage(args ...interface{}) string {
	// parse locale/kind/data
	locale := LocaleVI
	kind := ""
	var data interface{}
	for _, a := range args {
		switch v := a.(type) {
		case Locale:
			locale = v
		case string:
			// could be locale or kind; if valid locale, treat as locale
			if Locale(v).IsValid() {
				locale = Locale(v)
			} else if kind == "" {
				kind = v
			} else {
				// data as string? ignore
			}
		default:
			data = v
		}
	}
	if !locale.IsValid() {
		locale = LocaleVI
	}
	nk := normalizeKind(kind)
	// position_closed: choose template by pnl sign
	if nk == MsgPositionClosed {
		m := toMap(data)
		netVal := lookup(m, "net")
		if netVal == "" {
			netVal = lookup(m, "pnl")
		}
		if isNegative(netVal) {
			nk = MsgStopLoss
		} else {
			nk = MsgTakeProfit
		}
	}
	tplMap, ok := templates[nk]
	if !ok {
		// try direct
		if t, ok2 := templates[kind]; ok2 {
			tplMap = t
		}
	}
	if tplMap == nil {
		return ""
	}
	tpl, ok := tplMap[locale]
	if !ok {
		tpl = tplMap[LocaleVI]
	}
	m := toMap(data)
	return placeholderRe.ReplaceAllStringFunc(tpl, func(ph string) string {
		key := strings.Trim(ph, "{}")
		if strings.TrimSpace(key) == "HH:MM DD/MM" {
			return resolveMessageTime(m).Format("15:04 02/01")
		}
		val := lookup(m, key)
		if val == "" {
			return ph
		}
		return val
	})
}

func resolveMessageTime(m map[string]string) time.Time {
	for _, k := range []string{"time", "Time", "timestamp", "Timestamp", "occurred_at", "at", "datetime", "Datetime"} {
		if v, ok := m[k]; ok && strings.TrimSpace(v) != "" {
			v = strings.TrimSpace(v)
			for _, layout := range []string{"15:04 02/01", time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05", "02/01 15:04"} {
				if t, err := time.Parse(layout, v); err == nil {
					return t
				}
			}
		}
	}
	return time.Now()
}

// Aliases
func Render(args ...interface{}) string         { return RenderMessage(args...) }
func RenderTemplate(args ...interface{}) string { return RenderMessage(args...) }
func Message(args ...interface{}) string        { return RenderMessage(args...) }
func Template(args ...interface{}) string       { return RenderMessage(args...) }
func FormatMessage(args ...interface{}) string  { return RenderMessage(args...) }
