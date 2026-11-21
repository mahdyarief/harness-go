package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func localGateway(t *testing.T, handler http.HandlerFunc) (*Gateway, *httptest.Server) {
	t.Helper()
	s := httptest.NewTLSServer(handler)
	g, err := NewGateway(config())
	if err != nil {
		t.Fatal(err)
	}
	g.config.Endpoint = s.URL
	g.client = s.Client()
	g.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return g, s
}
func provider(content string) string {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}}, "usage": map[string]int{"completion_tokens": 10}})
	return string(b)
}
func TestConnection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"text", 200, provider("The parcel is blue."), nil},
		{"different_json", 200, provider(`{"color":"blue"}`), nil},
		{"empty", 200, provider("  "), ErrOutput},
		{"invalid", 200, `{}`, ErrOutput},
		{"truncated", 200, `{"choices":[{"finish_reason":"length","message":{"content":"blue"}}]}`, ErrOutput},
		{"auth", 401, `{"error":"sensitive"}`, ErrProvider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, s := localGateway(t, func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Messages       []Message       `json:"messages"`
					ResponseFormat json.RawMessage `json:"response_format"`
					Tools          json.RawMessage `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if len(payload.Messages) != 2 || payload.Messages[1].Content != demoQuote || len(payload.ResponseFormat) != 0 || len(payload.Tools) != 0 {
					t.Error("kontrak uji koneksi berubah")
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			defer s.Close()
			if err := g.CheckConnection(context.Background()); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDemo(t *testing.T) {
	good := `{"label":"blue","evidence_id":"synthetic-1","quote":"The fictional parcel is blue."}`
	g, s := localGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-key" {
			t.Error("auth")
		}
		var v map[string]any
		if json.NewDecoder(r.Body).Decode(&v) != nil || v["max_tokens"] != float64(100) || v["response_format"] == nil {
			t.Error("request")
		}
		prompt, _ := json.Marshal(v["messages"])
		if !strings.Contains(string(prompt), "label, evidence_id, quote") {
			t.Error("schema tidak dijelaskan dalam prompt")
		}
		io.WriteString(w, provider(good))
	})
	defer s.Close()
	result, err := g.SyntheticDemo(context.Background())
	if err != nil || result.Label != "blue" {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestOutputGuard(t *testing.T) {
	values := []string{`null`, `{}`, `{"label":"approved","evidence_id":"synthetic-1","quote":"The fictional parcel is blue."}`, `{"label":"blue","evidence_id":"unknown","quote":"The fictional parcel is blue."}`, `{"label":"blue","evidence_id":"synthetic-1","quote":"modified"}`, `{"label":"abstain","evidence_id":"synthetic-1","quote":""}`, `{"label":"abstain","label":"blue","evidence_id":"","quote":""}`, `{"label":"abstain","evidence_id":"","quote":"","extra":1}`, `{"label":"abstain","evidence_id":"","quote":""} {}`, `{"label":"not_blue","evidence_id":"synthetic-1","quote":"The fictional parcel is blue."}`}
	for _, v := range values {
		t.Run(v, func(t *testing.T) {
			g, s := localGateway(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, provider(v)) })
			defer s.Close()
			out, err := g.SyntheticDemo(context.Background())
			if !errors.Is(err, ErrOutput) || out.Label != "abstain" {
				t.Fatalf("%+v %v", out, err)
			}
		})
	}
	g, s := localGateway(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, provider(`{"label":"abstain","evidence_id":"","quote":""}`))
	})
	defer s.Close()
	out, err := g.SyntheticDemo(context.Background())
	if err != nil || out.Label != "abstain" {
		t.Fatal(err)
	}
}
func TestProviderFailures(t *testing.T) {
	for _, v := range []string{`invalid secret-key`, `{"choices":[]}`, `{"choices":[{"finish_reason":"length","message":{"content":"secret-key"}}]}`, strings.Repeat("x", 5000)} {
		g, s := localGateway(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, v) })
		out, err := g.SyntheticDemo(context.Background())
		s.Close()
		if err == nil || out.Label != "abstain" || strings.Contains(err.Error(), "secret-key") {
			t.Fatal("unsafe failure")
		}
	}
	calls := 0
	g, s := localGateway(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "secret-key provider details", 429)
	})
	defer s.Close()
	_, err := g.SyntheticDemo(context.Background())
	if err != ErrProvider || calls != 1 {
		t.Fatalf("retries %d err %v", calls, err)
	}
}
func TestTimeoutAndCancellation(t *testing.T) {
	g, s := localGateway(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})
	defer s.Close()
	g.config.Timeout = 20 * time.Millisecond
	_, err := g.SyntheticDemo(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = g.SyntheticDemo(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestToolsAndRedirects(t *testing.T) {
	for _, name := range []string{"sql", "write", "patient", ""} {
		if _, err := runTool(name, json.RawMessage(`{}`)); err != ErrTool {
			t.Fatal(name)
		}
	}
	if _, err := runTool("synthetic_evidence", json.RawMessage(`{"text":"patient"}`)); err != ErrTool {
		t.Fatal("input")
	}
	hits := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer target.Close()
	g, s := localGateway(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) })
	defer s.Close()
	_, err := g.SyntheticDemo(context.Background())
	if err != ErrProvider || hits != 0 {
		t.Fatal("redirect")
	}
	prod, _ := NewGateway(config())
	tr := prod.client.Transport.(*http.Transport)
	if tr.Proxy != nil || !tr.DisableKeepAlives {
		t.Fatal("transport")
	}
}
