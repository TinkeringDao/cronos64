package llm

import (
	"os"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/lib/netguard"
)

// The tests answer from an httptest server on loopback, which the local-only dialer refuses unless told.
func TestMain(m *testing.M) {
	netguard.AllowLoopbackForTest.Store(true)
	os.Exit(m.Run())
}
