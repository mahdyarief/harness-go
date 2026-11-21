package ai

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"testing"
	"time"
)

func config() Config {
	return Config{Endpoint: "https://api.example.com/v1/chat/completions", AllowedHosts: []string{"api.example.com"}, Model: "model", APIKey: "secret-key", Timeout: time.Second, MaxTokens: 100, MaxResponseBytes: 4096}
}
func TestSecrets(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	a, err := SealSecret(key, []byte("credential"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := SealSecret(key, []byte("credential"))
	if bytes.Equal(a, b) {
		t.Fatal("nonce reused")
	}
	p, err := OpenSecret(key, a)
	if err != nil || string(p) != "credential" {
		t.Fatal("round trip")
	}
	for _, x := range [][]byte{nil, a[:4], append([]byte{2}, a[1:]...)} {
		if _, err := OpenSecret(key, x); err == nil {
			t.Fatal("invalid envelope")
		}
	}
	a[len(a)-1] ^= 1
	if _, err = OpenSecret(key, a); err == nil {
		t.Fatal("tampering")
	}
	if _, err = SealSecret(key[:31], nil); err == nil {
		t.Fatal("short key")
	}
	if _, err = OpenSecret(bytes.Repeat([]byte{9}, 32), b); err == nil {
		t.Fatal("wrong key")
	}
}
func TestConfig(t *testing.T) {
	urls := []string{"http://api.example.com", "https://evil.example.com", "https://api.example.com.evil.org", "https://user@api.example.com", "https://api.example.com?x=1", "https://api.example.com?", "https://api.example.com#", "https://127.0.0.1", "https://[::1]", "https://API.example.com", "https://api.example.com.", "https://api.example.com:", "https://api.example.com:99999"}
	for _, u := range urls {
		c := config()
		c.Endpoint = u
		if _, err := NewGateway(c); err == nil {
			t.Errorf("accepted %s", u)
		}
	}
	if _, err := NewGateway(config()); err != nil {
		t.Fatal(err)
	}
	c := config()
	c.MaxTokens = 0
	if _, err := NewGateway(c); err == nil {
		t.Fatal("budget")
	}
	// A host outside the allowlist is a distinct failure: the handler turns it
	// into its own message instead of the generic invalid-configuration one.
	c = config()
	c.Endpoint = "https://api.evil.org/v1"
	if _, err := NewGateway(c); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("want ErrHostNotAllowed, got %v", err)
	}
}
func TestResolveEndpoint(t *testing.T) {
	// An OpenAI-compatible base path gains the chat completions route; a full
	// endpoint is left untouched.
	for in, want := range map[string]string{
		"https://api.example.com/v1":                  "https://api.example.com/v1/chat/completions",
		"https://api.example.com/v1/":                 "https://api.example.com/v1/chat/completions",
		"https://api.example.com":                     "https://api.example.com/v1/chat/completions",
		"https://api.example.com/v1/chat/completions": "https://api.example.com/v1/chat/completions",
	} {
		u, err := url.Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := resolveEndpoint(u); got != want {
			t.Errorf("resolveEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}
func TestDialChecked(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "224.0.0.1", "0.0.0.0", "::1", "fe80::1", "fc00::1", "::ffff:8.8.8.8", "100.64.0.1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Error(ip)
		}
	}
	called := false
	dial := func(_ context.Context, _, addr string) (net.Conn, error) {
		called = true
		if addr != "8.8.8.8:443" {
			t.Error(addr)
		}
		return nil, errors.New("offline")
	}
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	if _, err := dialChecked(context.Background(), "tcp", "api.example.com:443", lookup, dial); err == nil || called {
		t.Fatal("mixed DNS accepted")
	}
	lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	_, _ = dialChecked(context.Background(), "tcp", "api.example.com:443", lookup, dial)
	if !called {
		t.Fatal("not pinned")
	}
}
