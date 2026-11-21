package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOptions(t *testing.T) {
	base := []string{"--dsn", "postgres://runtime:secret@127.0.0.1/disposable", "--actor", "11111111-1111-1111-1111-111111111111", "--queries", "queries.json"}
	if _, err := parseOptions(base); err != nil {
		t.Fatal(err)
	}
	for _, extra := range [][]string{{"--concurrency", "26"}, {"--concurrency", "0"}, {"--iterations", "10001"}, {"--iterations", "0"}, {"--actor", "bad"}, {"--tenant", "bad"}, {"--timeout", "11m"}, {"--request-timeout", "31s"}, {"--limit", "51"}, {"--timeout", "1s"}, {"unexpected"}} {
		if _, err := parseOptions(append(append([]string{}, base...), extra...)); err == nil {
			t.Fatalf("accepted %v", extra)
		}
	}
	if _, err := parseOptions(nil); err == nil {
		t.Fatal("missing required flags")
	}
}

func TestQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queries.json")
	for _, raw := range []string{`[]`, `null`, `[null]`, `[1]`, `[""]`, `["  "]`, `["ok"] {}`, `["` + strings.Repeat("x", 501) + `"]`, "[\"\xff\"]"} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadQueries(path); err == nil {
			t.Fatalf("accepted invalid fixture %q", raw)
		}
	}
	if err := os.WriteFile(path, []byte(`["alpha","beta"]`), 0600); err != nil {
		t.Fatal(err)
	}
	q, hash, err := loadQueries(path)
	if err != nil || len(q) != 2 || len(hash) != 64 {
		t.Fatalf("%v %s %v", q, hash, err)
	}
}

func TestPercentiles(t *testing.T) {
	values := []time.Duration{}
	for i := 1; i <= 100; i++ {
		values = append(values, time.Duration(i)*time.Millisecond)
	}
	if percentile(nil, .5) != 0 || percentile(values, .5) != 50 || percentile(values, .95) != 95 || percentile(values, .99) != 99 {
		t.Fatal("nearest-rank percentiles")
	}
}

func TestRunner(t *testing.T) {
	var active, peak atomic.Int32
	search := func(ctx context.Context, query string) (int, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing per-request deadline")
		}
		if query == "bad" {
			return 0, errors.New("private query/DSN never reported")
		}
		return 2, nil
	}
	r := runBenchmark(context.Background(), []string{"ok", "bad"}, 20, 3, time.Second, search)
	if r.Attempts != 40 || r.Successes != 20 || r.Errors != 20 || r.Hits != 40 || peak.Load() > 3 || r.QPS <= 0 || r.P99MS < r.P50MS {
		t.Fatalf("%+v peak=%d", r, peak.Load())
	}
}

func TestRunnerDeadlinesAndCancellation(t *testing.T) {
	search := func(ctx context.Context, _ string) (int, error) { <-ctx.Done(); return 0, ctx.Err() }
	r := runBenchmark(context.Background(), []string{"x"}, 2, 1, time.Millisecond, search)
	if r.Errors != 2 || r.Attempts != 2 {
		t.Fatalf("%+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = runBenchmark(ctx, []string{"x"}, 10000, 25, time.Second, search)
	if !r.Cancelled || r.Attempts != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestExecuteRejectsRemoteAndRedacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queries.json")
	if err := os.WriteFile(path, []byte(`["privatequery"]`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, dsn := range []string{"postgres://runtime:password@192.0.2.1/kbeval?sslmode=disable", "postgres://%invalid-password", "postgres://runtime:password@127.0.0.1,192.0.2.1/kbeval?sslmode=disable"} {
		var out bytes.Buffer
		code := execute(context.Background(), []string{"--dsn", dsn, "--actor", "11111111-1111-1111-1111-111111111111", "--queries", path}, &out)
		if code == 0 || strings.Contains(out.String(), "password") || strings.Contains(out.String(), "privatequery") || strings.Contains(out.String(), dsn) {
			t.Fatalf("code=%d output=%q", code, out.String())
		}
	}
}
