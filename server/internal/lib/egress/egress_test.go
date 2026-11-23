package egress

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func mustAddr(t *testing.T, raw string) netip.Addr {
	t.Helper()
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func TestValidAddresses(t *testing.T) {
	public4 := mustAddr(t, "93.184.216.34")
	private4 := mustAddr(t, "10.0.0.5")
	loopback := mustAddr(t, "127.0.0.1")
	linkLocal := mustAddr(t, "169.254.1.1")
	reserved := mustAddr(t, "192.0.2.10")
	ula := mustAddr(t, "fd00::1")
	global6 := mustAddr(t, "2606:4700::1111")
	cases := []struct {
		name         string
		ips          []netip.Addr
		allowPrivate bool
		plainHTTP    bool
		want         bool
	}{
		{"public over TLS", []netip.Addr{public4}, false, false, true},
		{"public over plain HTTP", []netip.Addr{public4}, false, true, false},
		{"private rejected by default", []netip.Addr{private4}, false, false, false},
		{"private allowed explicitly", []netip.Addr{private4}, true, false, true},
		{"loopback follows private rule", []netip.Addr{loopback}, true, false, true},
		{"link local rejected", []netip.Addr{linkLocal}, false, false, false},
		{"reserved block rejected", []netip.Addr{reserved}, false, false, false},
		{"unique local v6 follows private", []netip.Addr{ula}, true, false, true},
		{"global v6 accepted", []netip.Addr{global6}, false, false, true},
		{"mixed answer rejected if one fails", []netip.Addr{public4, private4}, false, false, false},
		{"empty answer rejected", nil, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidAddresses(tc.ips, tc.allowPrivate, tc.plainHTTP); got != tc.want {
				t.Fatalf("ValidAddresses = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewRejectsUnlistedHostBeforeResolving(t *testing.T) {
	policy := Policy{AllowedHosts: []string{"allowed.example"}, Timeout: time.Second}
	if _, _, err := New(policy, "https://blocked.example/v1"); Code(err) != "invalid_endpoint" {
		t.Fatalf("err = %v, code = %q", err, Code(err))
	}
}

func TestNewRejectsMalformedTargets(t *testing.T) {
	policy := Policy{AllowedHosts: []string{"example.com"}, Timeout: time.Second}
	for _, target := range []string{
		"https://user:pass@example.com/v1",
		"https://example.com/v1?x=1",
		"https://example.com/v1#frag",
		"ftp://example.com/v1",
	} {
		if _, _, err := New(policy, target); Code(err) != "invalid_endpoint" {
			t.Fatalf("%s: err = %v", target, err)
		}
	}
}

func TestNewPinsDialToResolvedAddress(t *testing.T) {
	// localhost resolves locally, so this exercises the pin without egress.
	policy := Policy{AllowedHosts: []string{"localhost"}, AllowPrivate: true, Timeout: 2 * time.Second}
	endpoint, transport, err := New(policy, "https://localhost/v1/embeddings")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !strings.HasPrefix(endpoint, "https://localhost") {
		t.Fatalf("endpoint = %q", endpoint)
	}
	if transport.Proxy != nil {
		t.Fatal("transport must not use a proxy")
	}
	// A dial for any other host:port is refused before it reaches the network.
	if _, err := transport.DialContext(context.Background(), "tcp", "evil.example:443"); Code(err) != "unexpected_target" {
		t.Fatalf("err = %v, code = %q", err, Code(err))
	}
}

func TestDialPinnedNeverRebinds(t *testing.T) {
	ips := []netip.Addr{mustAddr(t, "203.0.113.9"), mustAddr(t, "93.184.216.34")}
	var dialed []string
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		dialed = append(dialed, network+" "+address)
		if len(dialed) == 1 {
			return nil, errors.New("refused")
		}
		return nil, errors.New("stop")
	}
	_, err := DialPinned(context.Background(), "tcp", "443", ips, dial)
	if Code(err) != "connection_failed" || !IsRetryable(err) {
		t.Fatalf("err = %v, retryable = %v", err, IsRetryable(err))
	}
	want := []string{"tcp 203.0.113.9:443", "tcp 93.184.216.34:443"}
	if len(dialed) != len(want) {
		t.Fatalf("dialed = %v", dialed)
	}
	for i := range want {
		if dialed[i] != want[i] {
			t.Fatalf("dialed[%d] = %q, want %q", i, dialed[i], want[i])
		}
	}
}

func TestDialPinnedStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	dial := func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("refused")
	}
	_, err := DialPinned(ctx, "tcp", "443", []netip.Addr{mustAddr(t, "93.184.216.34")}, dial)
	if Code(err) != "connection_failed" || calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
}

func TestErrorHelpers(t *testing.T) {
	policyErr := bad("invalid_endpoint")
	if policyErr.Error() != "egress: invalid_endpoint" {
		t.Fatalf("Error() = %q", policyErr.Error())
	}
	if Code(policyErr) != "invalid_endpoint" || IsRetryable(policyErr) {
		t.Fatal("policy rejection must not be retryable")
	}
	if Code(errors.New("plain")) != "" || IsRetryable(errors.New("plain")) {
		t.Fatal("foreign errors carry no egress code")
	}
}
