package wyoming

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/netguard"
)

// A speech server is reached only on the LAN or an overlay mesh: a public address is refused before
// any packet is sent, and loopback is refused once the test hook is off.
func TestVoicesRefusesWhatIsNotOnTheLAN(t *testing.T) {
	addr := fakeServer(t, pipersInfo) // loopback
	netguard.AllowLoopbackForTest.Store(false)
	defer netguard.AllowLoopbackForTest.Store(true)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Voices(ctx, addr, "en"); err == nil {
		t.Fatal("a server on loopback answered")
	}
	start := time.Now()
	_, err := Voices(ctx, "8.8.8.8:10200", "en")
	if err == nil || !strings.Contains(err.Error(), "non-local destination refused") {
		t.Fatalf("err = %v, want non-local destination refused", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("refusal took %v: a connection was attempted", time.Since(start))
	}
}

func TestDialRefusesPublicDestination(t *testing.T) {
	netguard.AllowLoopbackForTest.Store(false)
	defer netguard.AllowLoopbackForTest.Store(true)
	c, err := dial(context.Background(), "1.1.1.1:10300")
	if c != nil {
		c.close()
		t.Fatal("connected to a public address")
	}
	if err == nil || !strings.Contains(err.Error(), "non-local destination refused") {
		t.Fatalf("err = %v", err)
	}
}
