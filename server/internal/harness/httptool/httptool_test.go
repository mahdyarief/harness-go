package httptool

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hsg/internal/harness/services"
)

func passthrough(_ int, body json.RawMessage) (services.ToolResult, error) {
	return services.ToolResult{Output: string(body)}, nil
}

func TestNewRejectsUnknownHost(t *testing.T) {
	if _, err := New(Spec{Name: "x", URL: "https://example.com", AllowedHosts: []string{"other.com"}, Map: passthrough}); err == nil {
		t.Fatal("a host outside the allowlist must fail at mount")
	}
	if _, err := New(Spec{Name: "x", Map: passthrough}); err == nil {
		t.Fatal("a spec without a URL must fail at mount")
	}
	if _, err := New(Spec{Name: "x", URL: "https://example.com", Map: nil}); err == nil {
		t.Fatal("a spec without Map must fail at mount")
	}
	if _, err := New(Spec{Name: "x", URL: "https://example.com", Method: "DELETE", Map: passthrough}); err == nil {
		t.Fatal("a mutating method must fail at mount")
	}
}

func TestPrivateRejectedUnlessAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	name := strings.Split(host, ":")[0]
	if _, err := New(Spec{Name: "x", URL: srv.URL, AllowedHosts: []string{name}, Map: passthrough}); err == nil {
		t.Fatal("plain HTTP to a private address must fail without AllowPlainHTTP")
	}
	tool, err := New(Spec{
		Name: "x", URL: srv.URL, AllowedHosts: []string{name},
		AllowPlainHTTP: true, AllowPrivate: true, Map: passthrough,
	})
	if err != nil {
		t.Fatalf("allowed dev target: %v", err)
	}
	out, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.Output, `"ok":true`) {
		t.Fatalf("output = %s", out.Output)
	}
}

func TestServiceDownIsAResultNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	url := srv.URL
	srv.Close()
	host := strings.Split(strings.TrimPrefix(url, "http://"), ":")[0]
	tool, err := New(Spec{
		Name: "down", URL: url,
		AllowedHosts:   []string{host},
		AllowPlainHTTP: true, AllowPrivate: true,
		Timeout: 300 * time.Millisecond, Map: passthrough,
	})
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	out, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("a down service must not fail the run: %v", err)
	}
	if !strings.Contains(out.Output, "SERVICE_UNAVAILABLE") {
		t.Fatalf("output = %s", out.Output)
	}
}

func TestNon2xxReachesMap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"nope"}`))
	}))
	defer srv.Close()
	host := strings.Split(strings.TrimPrefix(srv.URL, "http://"), ":")[0]
	tool, err := New(Spec{
		Name: "x", URL: srv.URL, AllowedHosts: []string{host},
		AllowPlainHTTP: true, AllowPrivate: true,
		Map: func(status int, body json.RawMessage) (services.ToolResult, error) {
			if status != http.StatusNotFound {
				t.Fatalf("status = %d", status)
			}
			return services.ToolResult{Output: "status " + string(rune(status)) + " " + string(body)}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "nope") {
		t.Fatalf("output = %s", out.Output)
	}
}

func TestOversizedResponseIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("a"), MaxResponseBytes+10))
	}))
	defer srv.Close()
	host := strings.Split(strings.TrimPrefix(srv.URL, "http://"), ":")[0]
	tool, err := New(Spec{
		Name: "x", URL: srv.URL, AllowedHosts: []string{host},
		AllowPlainHTTP: true, AllowPrivate: true, Map: passthrough,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "SERVICE_UNAVAILABLE") {
		t.Fatalf("output = %s", out.Output)
	}
}

func TestRetryOnRetryableFailure(t *testing.T) {
	attempts := 0
	closed := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 2 {
			closed++
			panic("dial closed")
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer func() {
		srv.Close()
		recover()
		_ = closed
	}()
	host := strings.Split(strings.TrimPrefix(srv.URL, "http://"), ":")[0]
	tool, err := New(Spec{
		Name: "x", URL: srv.URL, AllowedHosts: []string{host},
		AllowPlainHTTP: true, AllowPrivate: true, Retries: 1, Map: passthrough,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
}

func TestReadOnly(t *testing.T) {
	tool, err := New(Spec{
		Name: "x", URL: "https://example.com", AllowedHosts: []string{"example.com"}, Map: passthrough,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !tool.ReadOnly() {
		t.Fatal("the http tool must be read-only")
	}
}
