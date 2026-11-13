package netguard

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestLocalOnlyDialerBlocksPublicAndAllowsLANOverlay(t *testing.T) {
	cases := []struct {
		ip   net.IP
		want bool
	}{
		{net.IPv4(192, 168, 1, 20), true},
		{net.IPv4(10, 42, 0, 5), true},
		{net.IPv4(172, 20, 1, 9), true},
		{net.IPv4(100, 101, 102, 103), true},     // RFC 6598 overlay (Tailscale/WireGuard)
		{net.ParseIP("fd7a:115c:a1e0::1"), true}, // IPv6 ULA
		{net.IPv4(8, 8, 8, 8), false},
		{net.IPv4(1, 1, 1, 1), false},
		{net.IPv4(100, 128, 0, 1), false}, // outside RFC 6598 /10
		{net.IPv4(172, 32, 0, 1), false},  // outside 172.16.0.0/12
		{net.ParseIP("2606:4700:4700::1111"), false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := IsLANOrOverlay(tc.ip); got != tc.want {
			t.Fatalf("IsLANOrOverlay(%v) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

func TestLocalOnlyDialerRefusesPublicDestination(t *testing.T) {
	d := LocalOnlyDialer(time.Second)
	start := time.Now()
	conn, err := d.DialContext(context.Background(), "tcp", "8.8.8.8:443")
	if conn != nil {
		conn.Close()
		t.Fatal("connected to a public address")
	}
	if err == nil || !strings.Contains(err.Error(), "non-local destination refused") {
		t.Fatalf("err = %v, want non-local destination refused", err)
	}
	if time.Since(start) > 900*time.Millisecond {
		t.Fatalf("refusal took %v: a connection was attempted", time.Since(start))
	}
}

func TestLocalOnlyDialerLoopbackOnlyWhenAllowedForTest(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	AllowLoopbackForTest.Store(false)
	if c, err := LocalOnlyDialer(time.Second).DialContext(context.Background(), "tcp", ln.Addr().String()); err == nil {
		c.Close()
		t.Fatal("loopback reached with the test hook off")
	}

	AllowLoopbackForTest.Store(true)
	defer AllowLoopbackForTest.Store(false)
	c, err := LocalOnlyDialer(time.Second).DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("loopback refused with the test hook on: %v", err)
	}
	c.Close()
}
