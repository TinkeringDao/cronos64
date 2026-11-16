package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/netguard"
)

func TestHindsightRecallAndAsyncRetain(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	netguard.AllowLoopbackForTest.Store(true)
	defer netguard.AllowLoopbackForTest.Store(false)

	var (
		recalledReachBank atomic.Bool
		retainedWrite     atomic.Bool
		retainedAsync     atomic.Bool
		sawMemoryInPrompt atomic.Bool
	)
	retainDone := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/default/banks/reachy/memories/list":
			recalledReachBank.Store(true)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"text":"Reachy Mini is a bilingual desk companion with twin spring-coil antennas."}]}`))
		case "/v1/default/banks/companion_write/memories/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"text":"Companion runs on Echo Show 5 Gen 2 cronos hardware."}]}`))
		case "/v1/default/banks/companion_write/memories":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if async, _ := body["async"].(bool); async {
				retainedAsync.Store(true)
			}
			retainedWrite.Store(true)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			select {
			case retainDone <- struct{}{}:
			default:
			}
		case "/v1/chat/completions":
			var req struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			for _, m := range req.Messages {
				if m.Role == "system" && strings.Contains(m.Content, "bilingual desk companion with twin spring-coil antennas") {
					sawMemoryInPrompt.Store(true)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"I am Reachy Mini, your bilingual desk companion!"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if err := config.Set().Brain().Set(config.Brain{
		Mode:               config.BrainDirect,
		STT:                "127.0.0.1:10300",
		TTS:                "127.0.0.1:10200",
		LLM:                srv.URL + "/v1",
		Model:              "test-model",
		Hindsight:          srv.URL + "/v1/default",
		HindsightBank:      "reachy",
		HindsightWriteBank: "companion_write",
	}); err != nil {
		t.Fatal(err)
	}

	reply, err := Get().Think(context.Background(), "Who are you, Reachy?")
	if err != nil {
		t.Fatalf("Think failed: %v", err)
	}
	if reply != "I am Reachy Mini, your bilingual desk companion!" {
		t.Fatalf("reply = %q, want expected greeting", reply)
	}
	if !recalledReachBank.Load() {
		t.Fatal("expected Think to recall from Hindsight reachy bank")
	}
	if !sawMemoryInPrompt.Load() {
		t.Fatal("expected recalled Hindsight memory to be injected into LLM system prompt")
	}

	select {
	case <-retainDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async Hindsight retain to companion_write bank")
	}
	if !retainedWrite.Load() || !retainedAsync.Load() {
		t.Fatalf("retainedWrite=%v retainedAsync=%v, want both true", retainedWrite.Load(), retainedAsync.Load())
	}
}
