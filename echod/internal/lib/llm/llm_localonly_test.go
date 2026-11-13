package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/lib/netguard"
)

// With no client of its own, a Client reaches no public address, and not loopback either once the
// test hook is off.
func TestChatRefusesWhatIsNotOnTheLAN(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(answer))
	}))
	defer srv.Close()

	netguard.AllowLoopbackForTest.Store(false)
	defer netguard.AllowLoopbackForTest.Store(true)

	for _, base := range []string{srv.URL + "/v1", "http://8.8.8.8/v1"} {
		c := &Client{Base: base}
		_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
		if err == nil {
			t.Fatalf("%s answered", base)
		}
		if strings.HasPrefix(base, "http://8.8.8.8") && !strings.Contains(err.Error(), "non-local destination refused") {
			t.Errorf("%s: %v", base, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("the loopback server was reached %d times", hits.Load())
	}
}
