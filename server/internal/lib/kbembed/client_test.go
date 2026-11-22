package kbembed

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"

	"hsg/internal/lib/egress"
	"sync/atomic"
	"testing"
	"time"
)

const revision = "0123456789abcdef0123456789abcdef01234567"

func testConfig(endpoint string) Config {
	return Config{Endpoint: endpoint, Model: model, Revision: revision, Dimensions: dimensions,
		Credential: "test-secret", AllowedHosts: []string{"127.0.0.1"}, AllowPrivate: true}
}
func vectorBody(count int) []byte {
	data := make([]any, count)
	for i := range data {
		vector := make([]float64, dimensions)
		vector[i] = 1
		data[count-1-i] = map[string]any{"object": "embedding", "index": i, "embedding": vector}
	}
	body, _ := json.Marshal(map[string]any{"object": "list", "model": model, "data": data})
	return body
}
func TestEmbedContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Method != "POST" {
			t.Error("wrong route")
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" || r.Header.Get("X-Embedding-Revision") != revision {
			t.Error("wrong headers")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		inputs := body["input"].([]any)
		if inputs[0] != " query " || inputs[1] != "document" || body["model"] != model || body["dimensions"] != float64(dimensions) {
			t.Error("input changed")
		}
		w.Header().Set("X-Embedding-Revision", revision)
		w.Write(vectorBody(2))
	}))
	defer server.Close()
	for _, suffix := range []string{"", "/v1", "/v1/", "/v1/embeddings", "/v1/embeddings/"} {
		client, err := New(testConfig(server.URL + suffix))
		if err != nil {
			t.Fatal(err)
		}
		vectors, err := client.Embed(context.Background(), []string{" query ", "document"})
		if err != nil {
			t.Fatal(err)
		}
		if len(vectors) != 2 || len(vectors[0]) != 1024 || vectors[0][0] != 1 || vectors[1][1] != 1 {
			t.Fatal("bad vectors/order")
		}
	}
}

func TestTransportAndLimits(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	c, err := New(testConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Embed(context.Background(), []string{"text"}); err == nil || hits.Load() != 0 {
		t.Fatal("followed redirect")
	}
	for _, endpoint := range []string{server.URL + "/v1/chat/completions", server.URL + "?token=secret", server.URL + "#x", strings.Replace(server.URL, "http://", "http://user:pass@", 1)} {
		if _, err = New(testConfig(endpoint)); err == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.AllowPrivate = false }, func(c *Config) { c.AllowedHosts = nil },
		func(c *Config) { c.Revision = "main" }, func(c *Config) { c.Dimensions = 768 },
		func(c *Config) { c.Credential = "" }, func(c *Config) { c.Credential = "secret\n" },
		func(c *Config) { c.MaxBatchSize = 33 }, func(c *Config) { c.Timeout = 121 * time.Second },
	} {
		config := testConfig(server.URL)
		mutate(&config)
		if _, err = New(config); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	for _, input := range [][]string{nil, {" "}, {string([]byte{255})}, {strings.Repeat("x", 16385)}, make([]string, 33)} {
		if _, err = c.Embed(context.Background(), input); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	config := testConfig(server.URL)
	config.MaxRequestBytes = 100
	c, err = New(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Embed(context.Background(), []string{strings.Repeat("<", 20)}); err == nil {
		t.Fatal("JSON byte bound ignored")
	}
	config = testConfig(server.URL)
	config.Timeout = 10 * time.Millisecond
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer slow.Close()
	config.Endpoint = slow.URL
	c, err = New(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Embed(context.Background(), []string{"text"}); err == nil || !IsRetryable(err) {
		t.Fatal("timeout ignored")
	}
}

func TestAddressPolicyAndPinning(t *testing.T) {
	for _, tc := range []struct {
		ip                 string
		private, plain, ok bool
	}{
		{"8.8.8.8", false, false, true}, {"8.8.8.8", true, true, false},
		{"127.0.0.1", false, false, false}, {"127.0.0.1", true, true, true},
		{"10.0.0.1", true, true, true}, {"fc00::1", true, true, true},
		{"169.254.169.254", true, false, false}, {"100.100.100.200", true, false, false},
		{"192.0.2.1", true, false, false}, {"::ffff:127.0.0.1", true, false, false},
		{"::", true, false, false}, {"ff02::1", true, false, false},
	} {
		if got := egress.ValidAddresses([]netip.Addr{netip.MustParseAddr(tc.ip)}, tc.private, tc.plain); got != tc.ok {
			t.Errorf("%s: %v", tc.ip, got)
		}
	}
	if egress.ValidAddresses([]netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, false, false) {
		t.Fatal("mixed DNS accepted")
	}
	ips := []netip.Addr{netip.MustParseAddr("10.0.0.1")}
	_, err := egress.DialPinned(context.Background(), "tcp", "8080", ips, func(_ context.Context, _, address string) (net.Conn, error) {
		if address != "10.0.0.1:8080" {
			t.Fatal("unpinned dial")
		}
		return nil, io.EOF
	})
	if err == nil {
		t.Fatal("dial failure ignored")
	}
}

func TestResponseFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		header string
		body   []byte
		retry  bool
	}{
		{"missing_revision", 200, "", vectorBody(1), false},
		{"wrong_revision", 200, strings.Repeat("a", 40), vectorBody(1), false},
		{"rate_limit", 429, revision, []byte("secret provider body"), true},
		{"unavailable", 503, revision, nil, true}, {"auth", 401, revision, nil, false},
		{"oversize", 200, revision, []byte(strings.Repeat("x", maxResponseBytes+1)), false},
		{"invalid_json", 200, revision, []byte("NaN secret provider body"), false},
		{"count", 200, revision, vectorBody(2), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.header != "" {
					w.Header().Set("X-Embedding-Revision", tc.header)
				}
				w.WriteHeader(tc.status)
				w.Write(tc.body)
			}))
			defer server.Close()
			c, err := New(testConfig(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Embed(context.Background(), []string{"text"})
			if err == nil || IsRetryable(err) != tc.retry || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe failure: %v", err)
			}
		})
	}
}

func TestVectorValidation(t *testing.T) {
	for _, value := range []string{"0", "2", "null", "1e999", "NaN", `"1"`} {
		vector := strings.Repeat("0,", 1023) + value
		body := fmt.Sprintf(`{"object":"list","model":%q,"data":[{"object":"embedding","index":0,"embedding":[%s]}]}`, model, vector)
		if _, err := decodeVectors([]byte(body), 1); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	for _, body := range []string{
		`{"object":"list","model":"BAAI/bge-m3","data":[]}`,
		strings.Replace(string(vectorBody(1)), `"index":0`, `"index":null`, 1),
		strings.Replace(string(vectorBody(1)), `"index":0`, `"index":2`, 1),
		strings.Replace(string(vectorBody(2)), `"index":1`, `"index":0`, 1),
		strings.Replace(string(vectorBody(1)), `"BAAI/bge-m3"`, `"other"`, 1),
	} {
		if _, err := decodeVectors([]byte(body), 1); err == nil {
			t.Fatal("accepted malformed response")
		}
	}
}
