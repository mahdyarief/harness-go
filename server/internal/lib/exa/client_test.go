package exa

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc, keys ...string) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	client, err := New(Config{
		Endpoint:   srv.URL,
		Keys:       keys,
		Timeout:    5 * time.Second,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client, srv
}

const okJSON = `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"hasil pencarian"}]}}`

func TestSearchKeylessSucceeds(t *testing.T) {
	var gotAuth string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Header.Get("Accept") != "application/json, text/event-stream" {
			t.Errorf("accept = %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(okJSON))
	}, "stored-key")
	text, err := client.Search(context.Background(), "berita", 5)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hasil pencarian" {
		t.Fatalf("text = %q", text)
	}
	if gotAuth != "" {
		t.Fatalf("keyless attempt sent Authorization %q", gotAuth)
	}
}

func TestSearchParsesSSE(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: " + okJSON + "\n\n"))
	})
	text, err := client.Search(context.Background(), "q", 3)
	if err != nil || text != "hasil pencarian" {
		t.Fatalf("text = %q err = %v", text, err)
	}
}

func TestSearchRotatesKeysAfterRefusal(t *testing.T) {
	var auths []string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		auths = append(auths, auth)
		if auth == "Bearer second" {
			_, _ = w.Write([]byte(okJSON))
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}, "first", "second")
	text, err := client.Search(context.Background(), "q", 5)
	if err != nil || text != "hasil pencarian" {
		t.Fatalf("text = %q err = %v", text, err)
	}
	want := []string{"", "Bearer first", "Bearer second"}
	if len(auths) != len(want) {
		t.Fatalf("attempts = %v", auths)
	}
	for i := range want {
		if auths[i] != want[i] {
			t.Fatalf("attempt %d = %q, want %q", i, auths[i], want[i])
		}
	}
}

func TestSearchRefusalWithoutKeysSurfacesStatus(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := client.Search(context.Background(), "q", 5)
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusTooManyRequests {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchDoesNotRotateOnServerError(t *testing.T) {
	attempts := 0
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadGateway)
	}, "first", "second")
	if _, err := client.Search(context.Background(), "q", 5); err == nil {
		t.Fatal("want error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestSearchRejectsToolError(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[]}}`))
	})
	if _, err := client.Search(context.Background(), "q", 5); !errors.Is(err, ErrTool) {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchRejectsOversizedResponse(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", MaxResponseBytes+1)))
	})
	if _, err := client.Search(context.Background(), "q", 5); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewRejectsInsecureEndpoint(t *testing.T) {
	for _, endpoint := range []string{"http://mcp.exa.ai/mcp", "https://mcp.exa.ai/mcp?x=1", "https://user@mcp.exa.ai/mcp"} {
		if _, err := New(Config{Endpoint: endpoint, Timeout: time.Second}); err == nil {
			t.Errorf("accepted %q", endpoint)
		}
	}
}

func TestSearchRejectsBadInput(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {})
	for _, query := range []string{"", "   ", strings.Repeat("a", maxQueryBytes+1)} {
		if _, err := client.Search(context.Background(), query, 5); !errors.Is(err, ErrInput) {
			t.Errorf("query len %d: %v", len(query), err)
		}
	}
}
