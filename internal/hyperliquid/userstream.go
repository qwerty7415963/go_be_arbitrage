package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	MaxSubsPerConn       = 30
	MaxOutboundPerSec    = 10
	DefaultUserStreamURL = "wss://api.hyperliquid.xyz/ws"
)

// UserStreamStats is a point-in-time snapshot.
type UserStreamStats struct {
	Connections         int
	ActiveConnections   int
	Subscriptions       int
	ActiveSubscriptions int
	Active              int
	Subs                int
	Reconnects          int64
	ReconnectCount      int64
	DroppedFrames       int64
	Dropped             int64
	Frames              int64
	Messages            int64
}

type userStreamConn struct {
	stream    *UserStream
	conn      any
	mu        sync.Mutex
	subs      map[string]bool
	done      chan struct{}
	closeOnce sync.Once
}

// UserStream satisfies trader.UpstreamDialer.
type UserStream struct {
	URL      string
	WsURL    string
	Endpoint string
	Addr     string

	Dial       any
	DialFunc   any
	Dialer     any
	DialerFunc any

	Sink         any
	EventSink    any
	Handler      any
	EventHandler any

	mu       sync.Mutex
	active   map[string]bool
	conns    []*userStreamConn
	lastSend time.Time

	reconnects    atomic.Int64
	droppedFrames atomic.Int64
	frames        atomic.Int64
}

func NewUserStream(args ...any) *UserStream {
	us := &UserStream{
		URL:      DefaultUserStreamURL,
		WsURL:    DefaultUserStreamURL,
		Endpoint: DefaultUserStreamURL,
		Addr:     DefaultUserStreamURL,
		active:   map[string]bool{},
	}
	for _, a := range args {
		switch v := a.(type) {
		case string:
			if v != "" {
				us.URL = v
				us.WsURL = v
				us.Endpoint = v
				us.Addr = v
			}
		default:
			rv := reflect.ValueOf(a)
			if rv.IsValid() && rv.Kind() == reflect.Func {
				if us.Dial == nil {
					us.Dial = a
				}
				if us.DialFunc == nil {
					us.DialFunc = a
				}
				if us.Dialer == nil {
					us.Dialer = a
				}
				if us.DialerFunc == nil {
					us.DialerFunc = a
				}
			} else {
				if us.Sink == nil {
					us.Sink = a
				}
				if us.EventSink == nil {
					us.EventSink = a
				}
				if us.Handler == nil {
					us.Handler = a
				}
				if us.EventHandler == nil {
					us.EventHandler = a
				}
			}
		}
	}
	return us
}

func (us *UserStream) url() string {
	for _, s := range []string{us.URL, us.WsURL, us.Endpoint, us.Addr} {
		if s != "" {
			return s
		}
	}
	return DefaultUserStreamURL
}

func (us *UserStream) dialFn() any {
	for _, c := range []any{us.Dial, us.DialFunc, us.Dialer, us.DialerFunc} {
		if c != nil {
			return c
		}
	}
	return nil
}

func (us *UserStream) callDial(ctx context.Context) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fn := us.dialFn()
	if fn == nil {
		d := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
		conn, _, err := d.DialContext(ctx, us.url(), nil)
		return conn, err
	}
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		return nil, fmt.Errorf("dial not func")
	}
	t := v.Type()
	var args []reflect.Value
	if t.NumIn() == 0 {
		args = nil
	} else if t.NumIn() == 1 {
		args = []reflect.Value{reflect.ValueOf(ctx)}
	} else {
		if t.In(0).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
			args = []reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(us.url())}
		} else {
			args = []reflect.Value{reflect.ValueOf(us.url()), reflect.ValueOf(ctx)}
		}
	}
	outs := v.Call(args)
	var conn any
	var err error
	if len(outs) >= 1 {
		conn = outs[0].Interface()
	}
	if len(outs) >= 2 {
		if e, ok := outs[1].Interface().(error); ok {
			err = e
		}
	}
	return conn, err
}

func (us *UserStream) Subscribe(ctx context.Context, addr string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	addr = strings.ToLower(strings.TrimSpace(addr))
	us.mu.Lock()
	if us.active == nil {
		us.active = map[string]bool{}
	}
	if us.active[addr] {
		us.mu.Unlock()
		return nil
	}
	var conn *userStreamConn
	for _, c := range us.conns {
		c.mu.Lock()
		if len(c.subs) < MaxSubsPerConn {
			conn = c
		}
		c.mu.Unlock()
		if conn != nil {
			break
		}
	}
	if conn == nil {
		raw, err := us.callDial(ctx)
		if err != nil {
			us.mu.Unlock()
			return err
		}
		conn = &userStreamConn{stream: us, conn: raw, subs: map[string]bool{}, done: make(chan struct{})}
		us.conns = append(us.conns, conn)
		go us.readLoop(ctx, conn)
	}
	conn.mu.Lock()
	conn.subs[addr] = true
	conn.mu.Unlock()
	us.active[addr] = true
	us.mu.Unlock()

	us.sendSubscribe(conn, addr)
	return nil
}

func (us *UserStream) Unsubscribe(addr string) error {
	addr = strings.ToLower(strings.TrimSpace(addr))
	us.mu.Lock()
	if !us.active[addr] {
		us.mu.Unlock()
		return nil
	}
	delete(us.active, addr)
	var conn *userStreamConn
	for _, c := range us.conns {
		c.mu.Lock()
		if c.subs[addr] {
			conn = c
		}
		c.mu.Unlock()
		if conn != nil {
			break
		}
	}
	var remaining int
	var doClose bool
	if conn != nil {
		conn.mu.Lock()
		delete(conn.subs, addr)
		remaining = len(conn.subs)
		doClose = remaining == 0
		conn.mu.Unlock()
		if doClose {
			us.conns = removeUserConn(us.conns, conn)
		}
	}
	us.mu.Unlock()

	if conn != nil {
		us.sendUnsubscribe(conn.conn, addr)
		if doClose {
			conn.closeOnce.Do(func() {
				connClose(conn.conn)
				close(conn.done)
			})
		}
	}
	return nil
}

func (us *UserStream) sendSubscribe(conn *userStreamConn, addr string) {
	frames := []map[string]any{
		{"method": "subscribe", "subscription": map[string]any{"type": "userFills", "user": addr, "aggregateByTime": false}},
		{"method": "subscribe", "subscription": map[string]any{"type": "orderUpdates", "user": addr}},
	}
	for _, f := range frames {
		us.throttle()
		_ = connWriteJSON(conn.conn, f)
		us.frames.Add(1)
	}
}

func (us *UserStream) sendUnsubscribe(conn any, addr string) {
	frames := []map[string]any{
		{"method": "unsubscribe", "subscription": map[string]any{"type": "userFills", "user": addr}},
		{"method": "unsubscribe", "subscription": map[string]any{"type": "orderUpdates", "user": addr}},
	}
	for _, f := range frames {
		us.throttle()
		_ = connWriteJSON(conn, f)
	}
}

func (us *UserStream) throttle() {
	us.mu.Lock()
	defer us.mu.Unlock()
	now := time.Now()
	if !us.lastSend.IsZero() {
		wait := (time.Second / time.Duration(MaxOutboundPerSec)) - now.Sub(us.lastSend)
		if wait > 0 {
			time.Sleep(wait)
		}
	}
	us.lastSend = time.Now()
}

func (us *UserStream) readLoop(ctx context.Context, conn *userStreamConn) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-conn.done:
			return
		default:
		}
		raw, err := readFrame(conn.conn)
		if err != nil {
			us.handleDrop(ctx, conn)
			return
		}
		us.frames.Add(1)
		us.dispatch(raw, conn)
	}
}

func (us *UserStream) handleDrop(ctx context.Context, dead *userStreamConn) {
	dead.mu.Lock()
	addrs := make([]string, 0, len(dead.subs))
	for a := range dead.subs {
		addrs = append(addrs, a)
	}
	dead.mu.Unlock()

	us.mu.Lock()
	us.conns = removeUserConn(us.conns, dead)
	us.mu.Unlock()
	connClose(dead.conn)

	go us.reconnect(ctx, addrs)
}

func (us *UserStream) reconnect(ctx context.Context, addrs []string) {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		us.mu.Lock()
		var toSub []string
		for _, a := range addrs {
			if us.active[a] {
				toSub = append(toSub, a)
			}
		}
		us.mu.Unlock()
		if len(toSub) == 0 {
			return
		}

		jitter := time.Duration(0)
		if backoff > 0 {
			jitter = time.Duration(rand.Int63n(int64(backoff/4) + 1))
		}
		if !sleepCtx(ctx, backoff+jitter) {
			return
		}

		raw, err := us.callDial(ctx)
		if err != nil {
			us.reconnects.Add(1)
			backoff = nextBackoff(backoff)
			continue
		}
		conn := &userStreamConn{stream: us, conn: raw, subs: map[string]bool{}, done: make(chan struct{})}
		us.mu.Lock()
		us.conns = append(us.conns, conn)
		for _, a := range toSub {
			conn.subs[a] = true
		}
		us.mu.Unlock()
		go us.readLoop(ctx, conn)
		for _, a := range toSub {
			us.sendSubscribe(conn, a)
		}
		us.reconnects.Add(1)
		return
	}
}

func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	if d < time.Second {
		d = time.Second
	}
	return d
}

func (us *UserStream) dispatch(raw []byte, conn *userStreamConn) {
	var env struct {
		Channel string          `json:"channel"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return
	}
	switch env.Channel {
	case "userFills":
		var msg WsUserFillsMsg
		if err := json.Unmarshal(env.Data, &msg); err != nil {
			return
		}
		addr := msg.User
		if addr == "" {
			conn.mu.Lock()
			if len(conn.subs) == 1 {
				for a := range conn.subs {
					addr = a
				}
			}
			conn.mu.Unlock()
		}
		addr = strings.ToLower(strings.TrimSpace(addr))
		for _, f := range msg.Fills {
			us.callSinkFill(addr, f)
		}
	case "orderUpdates":
		var updates []WsOrderUpdate
		if err := json.Unmarshal(env.Data, &updates); err != nil {
			var single WsOrderUpdate
			if err2 := json.Unmarshal(env.Data, &single); err2 == nil {
				updates = []WsOrderUpdate{single}
			} else {
				return
			}
		}
		conn.mu.Lock()
		targets := make([]string, 0, len(conn.subs))
		for a := range conn.subs {
			targets = append(targets, a)
		}
		conn.mu.Unlock()
		for _, u := range updates {
			if len(targets) == 1 {
				us.callSinkOrder(targets[0], u)
			} else if len(targets) > 1 {
				for _, t := range targets {
					us.callSinkOrder(t, u)
				}
			}
		}
	}
}

func (us *UserStream) callSinkFill(addr string, fill Fill) {
	sink := us.sink()
	if sink == nil {
		return
	}
	if s, ok := sink.(interface{ OnRawUserFill(string, Fill) bool }); ok {
		s.OnRawUserFill(addr, fill)
		return
	}
	if s, ok := sink.(interface{ OnRawUserFill(string, Fill) }); ok {
		s.OnRawUserFill(addr, fill)
		return
	}
}

func (us *UserStream) callSinkOrder(addr string, upd WsOrderUpdate) {
	sink := us.sink()
	if sink == nil {
		return
	}
	if s, ok := sink.(interface{ OnOrder(string, WsOrderUpdate) }); ok {
		s.OnOrder(addr, upd)
		return
	}
	if s, ok := sink.(interface{ OnOrder(string, OpenOrder) }); ok {
		s.OnOrder(addr, upd.Order)
		return
	}
	if s, ok := sink.(interface{ OnOrder(string, any) }); ok {
		s.OnOrder(addr, upd)
		return
	}
}

func (us *UserStream) sink() any {
	for _, c := range []any{us.Sink, us.EventSink, us.Handler, us.EventHandler} {
		if c != nil {
			return c
		}
	}
	return nil
}

func (us *UserStream) Stats() UserStreamStats {
	us.mu.Lock()
	conns := len(us.conns)
	subs := len(us.active)
	us.mu.Unlock()
	return UserStreamStats{
		Connections:         conns,
		ActiveConnections:   conns,
		Subscriptions:       subs,
		ActiveSubscriptions: subs,
		Active:              subs,
		Subs:                subs,
		Reconnects:          us.reconnects.Load(),
		ReconnectCount:      us.reconnects.Load(),
		DroppedFrames:       us.droppedFrames.Load(),
		Dropped:             us.droppedFrames.Load(),
		Frames:              us.frames.Load(),
		Messages:            us.frames.Load(),
	}
}

func removeUserConn(list []*userStreamConn, target *userStreamConn) []*userStreamConn {
	out := list[:0]
	for _, c := range list {
		if c != target {
			out = append(out, c)
		}
	}
	res := make([]*userStreamConn, len(out))
	copy(res, out)
	return res
}

func readFrame(conn any) ([]byte, error) {
	v := reflect.ValueOf(conn)
	if v.IsNil() {
		return nil, io.EOF
	}
	if m := v.MethodByName("ReadMessage"); m.IsValid() && m.Type().NumIn() == 0 {
		outs := m.Call(nil)
		if len(outs) >= 3 {
			b, _ := outs[1].Interface().([]byte)
			err, _ := outs[2].Interface().(error)
			if err != nil {
				return nil, err
			}
			return b, nil
		}
	}
	if m := v.MethodByName("Read"); m.IsValid() && m.Type().NumIn() == 0 && m.Type().NumOut() == 2 {
		outs := m.Call(nil)
		if b, ok := outs[0].Interface().([]byte); ok {
			err, _ := outs[1].Interface().(error)
			return b, err
		}
	}
	if m := v.MethodByName("Recv"); m.IsValid() && m.Type().NumIn() == 0 && m.Type().NumOut() == 2 {
		outs := m.Call(nil)
		if b, ok := outs[0].Interface().([]byte); ok {
			err, _ := outs[1].Interface().(error)
			return b, err
		}
	}
	for _, name := range []string{"Frames", "Messages", "ReadCh", "Inbound", "Ch", "C"} {
		if f := v.FieldByName(name); f.IsValid() {
			switch ch := f.Interface().(type) {
			case <-chan []byte:
				b, ok := <-ch
				if !ok {
					return nil, io.EOF
				}
				return b, nil
			case chan []byte:
				b, ok := <-ch
				if !ok {
					return nil, io.EOF
				}
				return b, nil
			}
		}
	}
	return nil, fmt.Errorf("no readable frame source")
}

func connWriteJSON(conn any, v any) error {
	rv := reflect.ValueOf(conn)
	if m := rv.MethodByName("WriteJSON"); m.IsValid() {
		outs := m.Call([]reflect.Value{reflect.ValueOf(v)})
		if len(outs) > 0 {
			if err, ok := outs[0].Interface().(error); ok {
				return err
			}
		}
		return nil
	}
	if m := rv.MethodByName("WriteMessage"); m.IsValid() {
		b, _ := json.Marshal(v)
		outs := m.Call([]reflect.Value{reflect.ValueOf(1), reflect.ValueOf(b)})
		if len(outs) > 0 {
			if err, ok := outs[0].Interface().(error); ok {
				return err
			}
		}
		return nil
	}
	for _, name := range []string{"SendCh", "Outbound", "WriteCh"} {
		if f := rv.FieldByName(name); f.IsValid() {
			if ch, ok := f.Interface().(chan any); ok {
				select {
				case ch <- v:
				default:
				}
				return nil
			}
			if ch, ok := f.Interface().(chan []byte); ok {
				b, _ := json.Marshal(v)
				select {
				case ch <- b:
				default:
				}
				return nil
			}
		}
	}
	return nil
}

func connClose(conn any) {
	rv := reflect.ValueOf(conn)
	if m := rv.MethodByName("Close"); m.IsValid() {
		m.Call(nil)
	}
}

func isFunc(v any) bool {
	return reflect.ValueOf(v).Kind() == reflect.Func
}
