package netguard

import (
	"fmt"
	"net"
	"sync/atomic"
	"syscall"
	"time"
)

// AllowLoopbackForTest lets LocalOnlyDialer reach loopback, so a test can point a client at an
// httptest server on 127.0.0.1. Production code never sets it.
var AllowLoopbackForTest atomic.Bool

// localNets is where a connection may go: the private ranges of RFC 1918, the shared address space
// of RFC 6598 that overlay meshes (Tailscale, WireGuard) use, and IPv6 unique local addresses.
var localNets = []*net.IPNet{
	{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},     // RFC 1918
	{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},  // RFC 1918
	{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)}, // RFC 1918
	{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)},  // RFC 6598
	{IP: net.ParseIP("fc00::"), Mask: net.CIDRMask(7, 128)},    // IPv6 ULA
}

// IsLANOrOverlay is whether ip is a private LAN or overlay-mesh address.
func IsLANOrOverlay(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range localNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// LocalOnlyDialer makes connections that are refused, as they are made, unless the address dialed is
// on the LAN or an overlay mesh and not one Refused turns away (this device's own addresses,
// link-local, multicast). Control runs after the name is resolved and before any packet is sent, once
// per address tried, so it holds whatever the name was, whatever DNS answered and after any redirect.
func LocalOnlyDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout: timeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if AllowLoopbackForTest.Load() && ip != nil && ip.IsLoopback() {
				return nil
			}
			if Refused(ip) {
				return fmt.Errorf("the device may not reach %s", host)
			}
			if !IsLANOrOverlay(ip) {
				return fmt.Errorf("non-local destination refused: %s", host)
			}
			return nil
		},
	}
}
