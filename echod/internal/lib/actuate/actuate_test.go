package actuate

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/netguard"
	"github.com/gorilla/websocket"
)

type spyTransport struct {
	mu     sync.Mutex
	poses  []Pose
	closed bool
}

func (s *spyTransport) Send(_ context.Context, p Pose) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.poses = append(s.poses, p)
	return nil
}

func (s *spyTransport) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func TestGuardedTransportClampsAnglesSetsTTLAndRateLimits(t *testing.T) {
	spy := &spyTransport{}
	sender := NewGuardedSender(spy)
	now := time.Unix(1700000000, 0)
	sender.now = func() time.Time { return now }

	ctx := context.Background()
	if err := sender.Send(ctx, Pose{
		Yaw:      99,
		Pitch:    -80,
		Roll:     60,
		Mood:     "happy",
		Speaking: 1.5,
		TTLMS:    0,
	}); err != nil {
		t.Fatalf("first Send error: %v", err)
	}

	// Within 33 ms window -> dropped silently, Seq must not advance.
	now = now.Add(15 * time.Millisecond)
	if err := sender.Send(ctx, Pose{Yaw: 10, Pitch: 10, Mood: "calm"}); err != nil {
		t.Fatalf("rate-limited Send error: %v", err)
	}

	// Past 33 ms window -> accepted with Seq 2 and clamped bounds.
	now = now.Add(20 * time.Millisecond) // +35 ms total
	if err := sender.Send(ctx, Pose{
		Yaw:      -120,
		Pitch:    42,
		Roll:     -75,
		Mood:     "listening",
		Speaking: -0.4,
		TTLMS:    250,
	}); err != nil {
		t.Fatalf("third Send error: %v", err)
	}

	if len(spy.poses) != 2 {
		t.Fatalf("got %d delivered poses, want 2 (1 dropped by 30 Hz limit)", len(spy.poses))
	}

	first := spy.poses[0]
	if first.Seq != 1 || first.Yaw != 45 || first.Pitch != -25 || first.Roll != 45 || first.TTLMS != 500 || first.Speaking != 1 {
		t.Fatalf("first pose = %+v, want Seq=1 Yaw=45 Pitch=-25 Roll=45 TTLMS=500 Speaking=1", first)
	}

	second := spy.poses[1]
	if second.Seq != 2 || second.Yaw != -45 || second.Pitch != 25 || second.Roll != -45 || second.TTLMS != 250 || second.Speaking != 0 {
		t.Fatalf("second pose = %+v, want Seq=2 Yaw=-45 Pitch=25 Roll=-45 TTLMS=250 Speaking=0", second)
	}

	if err := sender.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if !spy.closed {
		t.Fatal("expected underlying transport Close to be called")
	}
}

func TestGuardedSenderSanitizesNaNAndNilTransport(t *testing.T) {
	spy := &spyTransport{}
	sender := NewGuardedSender(spy)
	now := time.Unix(1700000100, 0)
	sender.now = func() time.Time { return now }

	if err := sender.Send(context.Background(), Pose{
		Yaw:      math.NaN(),
		Pitch:    math.Inf(1),
		Roll:     math.Inf(-1),
		Speaking: math.NaN(),
	}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(spy.poses) != 1 {
		t.Fatalf("got %d poses, want 1", len(spy.poses))
	}
	p := spy.poses[0]
	if p.Yaw != 0 || p.Pitch != 0 || p.Roll != 0 || p.Speaking != 0 || p.TTLMS != 500 || p.Seq != 1 {
		t.Fatalf("sanitized pose = %+v, want zeros with TTLMS=500 Seq=1", p)
	}

	noopSender := NewGuardedSender(nil)
	if err := noopSender.Send(context.Background(), Pose{Yaw: 5}); err != nil {
		t.Fatalf("nil transport Send failed: %v", err)
	}
	if err := noopSender.Close(); err != nil {
		t.Fatalf("nil transport Close failed: %v", err)
	}
}

func TestNoopTransport(t *testing.T) {
	var tr Transport = NoopTransport{}
	if err := tr.Send(context.Background(), Pose{Yaw: 12}); err != nil {
		t.Fatalf("NoopTransport.Send error: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("NoopTransport.Close error: %v", err)
	}
}

func TestWebSocketTransportRejectsNonLANAndSendsLocal(t *testing.T) {
	if _, err := NewWebSocketTransport("http://10.0.0.2:8765/ws"); err == nil {
		t.Fatal("expected error for non-ws scheme")
	}
	if _, err := NewWebSocketTransport("ws:///ws"); err == nil {
		t.Fatal("expected error for empty host")
	}

	// Non-LAN destination must be refused before packets leave.
	netguard.AllowLoopbackForTest.Store(false)
	defer netguard.AllowLoopbackForTest.Store(false)

	pubTr, err := NewWebSocketTransport("ws://8.8.8.8:8765/pose")
	if err == nil {
		err = pubTr.Send(context.Background(), Pose{Seq: 1, TTLMS: 500})
		_ = pubTr.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "non-local destination refused") {
		t.Fatalf("public URL error = %v, want non-local destination refused", err)
	}

	upgrader := websocket.Upgrader{}
	received := make(chan Pose, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read failed: %v", err)
			return
		}
		var got Pose
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("unmarshal failed: %v", err)
			return
		}
		received <- got
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/pose"

	// With loopback disallowed, loopback test server must also be refused.
	loopBlocked, err := NewWebSocketTransport(wsURL)
	if err == nil {
		err = loopBlocked.Send(context.Background(), Pose{Seq: 1, TTLMS: 500})
		_ = loopBlocked.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "may not reach") {
		t.Fatalf("loopback refused error = %v, want may not reach", err)
	}

	// With loopback allowed for testing, GuardedSender delivers clamped JSON pose over WS.
	netguard.AllowLoopbackForTest.Store(true)
	localTr, err := NewWebSocketTransport(wsURL)
	if err != nil {
		t.Fatalf("NewWebSocketTransport(local) error: %v", err)
	}
	defer localTr.Close()

	guarded := NewGuardedSender(localTr)
	if err := guarded.Send(context.Background(), Pose{Yaw: 60, Pitch: -30, Mood: "happy", Speaking: 0.75}); err != nil {
		t.Fatalf("guarded local WS Send error: %v", err)
	}

	select {
	case got := <-received:
		if got.Seq != 1 || got.Yaw != 45 || got.Pitch != -25 || got.Mood != "happy" || got.Speaking != 0.75 || got.TTLMS != 500 {
			t.Fatalf("WS received pose = %+v, want Seq=1 Yaw=45 Pitch=-25 Mood=happy Speaking=0.75 TTLMS=500", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for WebSocket pose frame")
	}
}
