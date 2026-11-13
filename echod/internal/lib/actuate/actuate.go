// Package actuate defines the clean seam for forwarding companion head poses
// and emotional state from echod to an optional local actuator (such as a Reachy).
// All outbound transports use netguard.LocalOnlyDialer with proxy support disabled.
package actuate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/netguard"
	"github.com/gorilla/websocket"
)

const (
	// MaxYawDeg is the symmetric mechanical yaw limit in degrees.
	MaxYawDeg = 45.0
	// MaxPitchDeg is the symmetric mechanical pitch limit in degrees.
	MaxPitchDeg = 25.0
	// MaxRollDeg is the symmetric mechanical roll limit in degrees.
	MaxRollDeg = 45.0
	// DefaultTTLMS is applied whenever a Pose leaves TTLMS <= 0.
	DefaultTTLMS = 500
	// MinFrameInterval enforces the 30 Hz outbound ceiling (< 33 ms drops).
	MinFrameInterval = 33 * time.Millisecond
)

// Pose describes a target companion head orientation and expression frame.
type Pose struct {
	Seq      uint64  `json:"seq"`
	Yaw      float64 `json:"yaw"`
	Pitch    float64 `json:"pitch"`
	Roll     float64 `json:"roll"`
	Mood     string  `json:"mood"`
	Speaking float64 `json:"speaking"`
	TTLMS    int     `json:"ttl_ms"`
}

// Transport delivers clamped, rate-limited Pose frames to a downstream target.
type Transport interface {
	Send(ctx context.Context, p Pose) error
	Close() error
}

// NoopTransport discards every pose frame cleanly with zero I/O.
type NoopTransport struct{}

// Send implements Transport.
func (NoopTransport) Send(context.Context, Pose) error { return nil }

// Close implements Transport.
func (NoopTransport) Close() error { return nil }

// GuardedSender enforces mechanical angle bounds, NaN/Inf sanitization, default
// TTLs, monotonic sequence numbering, and a 30 Hz outbound ceiling before
// forwarding Pose frames to an underlying Transport.
type GuardedSender struct {
	mu        sync.Mutex
	sendMu    sync.Mutex
	transport Transport
	lastSent  time.Time
	seq       uint64
	now       func() time.Time
}

// NewGuardedSender wraps transport with safety clamping and 30 Hz rate limiting.
// Nil transports fall back to NoopTransport.
func NewGuardedSender(t Transport) *GuardedSender {
	if t == nil {
		t = NoopTransport{}
	}
	return &GuardedSender{
		transport: t,
		now:       time.Now,
	}
}

// SetTransport swaps the downstream transport safely and returns the previous one
// without closing it synchronously under lock. Nil falls back to NoopTransport.
func (g *GuardedSender) SetTransport(t Transport) Transport {
	if t == nil {
		t = NoopTransport{}
	}
	g.mu.Lock()
	prev := g.transport
	g.transport = t
	g.lastSent = time.Time{}
	g.mu.Unlock()
	return prev
}

// Send clamps, rate-limits, numbers, and delivers p to the active transport.
// Frames arriving within MinFrameInterval of the previous accepted frame are
// silently dropped without advancing Seq.
func (g *GuardedSender) Send(ctx context.Context, p Pose) error {
	g.mu.Lock()
	ts := g.now()
	if !g.lastSent.IsZero() && ts.Sub(g.lastSent) < MinFrameInterval {
		g.mu.Unlock()
		return nil
	}
	g.lastSent = ts
	g.seq++
	p.Seq = g.seq
	t := g.transport
	g.mu.Unlock()

	p = clampPose(p)

	g.sendMu.Lock()
	defer g.sendMu.Unlock()
	return t.Send(ctx, p)
}

// Close closes the underlying transport and swaps to NoopTransport.
func (g *GuardedSender) Close() error {
	prev := g.SetTransport(NoopTransport{})
	g.sendMu.Lock()
	defer g.sendMu.Unlock()
	return prev.Close()
}

func clampPose(p Pose) Pose {
	p.Yaw = clampFinite(p.Yaw, -MaxYawDeg, MaxYawDeg)
	p.Pitch = clampFinite(p.Pitch, -MaxPitchDeg, MaxPitchDeg)
	p.Roll = clampFinite(p.Roll, -MaxRollDeg, MaxRollDeg)
	p.Speaking = clampFinite(p.Speaking, 0, 1)
	if p.TTLMS <= 0 {
		p.TTLMS = DefaultTTLMS
	}
	return p
}

func clampFinite(v, lo, hi float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// WebSocketTransport streams Pose JSON frames over a local/LAN WebSocket secured
// with netguard.LocalOnlyDialer and zero proxy routing.
type WebSocketTransport struct {
	mu     sync.Mutex
	rawURL string
	dialer websocket.Dialer
	conn   *websocket.Conn
	closed bool
}

// NewWebSocketTransport validates rawURL ("ws" or "wss" with a non-empty host)
// and prepares a local-only WebSocket transport without dialing on construction.
func NewWebSocketTransport(rawURL string) (*WebSocketTransport, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("actuate: parse websocket url: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, fmt.Errorf("actuate: unsupported scheme %q (want ws or wss)", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, errors.New("actuate: websocket url requires host")
	}
	return &WebSocketTransport{
		rawURL: rawURL,
		dialer: websocket.Dialer{
			NetDialContext:   netguard.LocalOnlyDialer(5 * time.Second).DialContext,
			Proxy:            nil,
			HandshakeTimeout: 5 * time.Second,
		},
	}, nil
}

// Send lazily dials the local/LAN endpoint on first use and writes p as JSON.
func (w *WebSocketTransport) Send(ctx context.Context, p Pose) error {
	payload, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("actuate: marshal pose: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("actuate: transport closed")
	}
	if w.conn == nil {
		conn, _, err := w.dialer.DialContext(ctx, w.rawURL, nil)
		if err != nil {
			return fmt.Errorf("actuate: dial %s: %w", w.rawURL, err)
		}
		w.conn = conn
	}
	_ = w.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := w.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		_ = w.conn.Close()
		w.conn = nil
		return fmt.Errorf("actuate: write pose: %w", err)
	}
	return nil
}

// Close closes any open WebSocket connection.
func (w *WebSocketTransport) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.conn == nil {
		return nil
	}
	err := w.conn.Close()
	w.conn = nil
	return err
}
