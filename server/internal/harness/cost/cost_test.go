package cost

import (
	"errors"
	"testing"
)

func testTable(t *testing.T) Table {
	t.Helper()
	table, err := NewTable("2026-02-01", map[string]Rates{
		"murah": {InputPerMillion: 2, OutputPerMillion: 8},
		"cache": {InputPerMillion: 3, OutputPerMillion: 9, CacheHitPerMillion: ptr(0.5)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func ptr(v float64) *float64 { return &v }

func approx(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-9
}

func TestCostSumsInputAndOutput(t *testing.T) {
	table := testTable(t)
	amount, err := table.Cost("murah", Usage{PromptTokens: 1_000_000, CompletionTokens: 500_000})
	if err != nil {
		t.Fatal(err)
	}
	if !amount.Known {
		t.Fatal("known model reported unknown")
	}
	if want := 1_000_000.0/1e6*2 + 500_000.0/1e6*8; !approx(amount.USD, want) {
		t.Fatalf("USD = %v; want %v", amount.USD, want)
	}
}

func TestCostPricesCacheHits(t *testing.T) {
	table := testTable(t)
	amount, err := table.Cost("cache", Usage{PromptTokens: 1_000_000, CompletionTokens: 0, CacheHitTokens: 400_000})
	if err != nil {
		t.Fatal(err)
	}
	if want := 600_000.0/1e6*3 + 400_000.0/1e6*0.5; !approx(amount.USD, want) {
		t.Fatalf("USD = %v; want %v", amount.USD, want)
	}
}

func TestCostFallsBackToInputRateWithoutCachePrice(t *testing.T) {
	table := testTable(t)
	amount, err := table.Cost("murah", Usage{PromptTokens: 1_000_000, CacheHitTokens: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if amount.USD != 2 {
		t.Fatalf("USD = %v; want 2", amount.USD)
	}
}

func TestCostClampsNegativeAndCachedOverflow(t *testing.T) {
	table := testTable(t)
	amount, err := table.Cost("murah", Usage{PromptTokens: -10, CompletionTokens: -5, CacheHitTokens: 99})
	if err != nil {
		t.Fatal(err)
	}
	if amount.USD != 0 {
		t.Fatalf("USD = %v; want 0", amount.USD)
	}
	cached, err := table.Cost("murah", Usage{PromptTokens: 100, CacheHitTokens: 500})
	if err != nil {
		t.Fatal(err)
	}
	if want := 100.0 / 1e6 * 2; cached.USD != want {
		t.Fatalf("USD = %v; want %v (cache cannot exceed prompt)", cached.USD, want)
	}
}

func TestCostUnknownModelIsANullAmount(t *testing.T) {
	table := testTable(t)
	amount, err := table.Cost("tidak-ada", Usage{PromptTokens: 100})
	if amount.Known || amount.USD != 0 {
		t.Fatalf("amount = %+v; want null", amount)
	}
	if err == nil || !IsUnknown(err) {
		t.Fatalf("err = %v; want UnknownModelError", err)
	}
	var unknown *UnknownModelError
	if !errors.As(err, &unknown) || unknown.Model != "tidak-ada" || unknown.TableVersion != "2026-02-01" {
		t.Fatalf("unknown = %+v", unknown)
	}
}

func TestNewTableRejectsInvalidRateCards(t *testing.T) {
	cases := map[string]struct {
		version string
		prices  map[string]Rates
	}{
		"no version":      {"", map[string]Rates{"a": {}}},
		"no prices":       {"v1", nil},
		"blank model":     {"v1", map[string]Rates{"  ": {}}},
		"negative input":  {"v1", map[string]Rates{"a": {InputPerMillion: -1}}},
		"negative output": {"v1", map[string]Rates{"a": {OutputPerMillion: -1}}},
		"negative cache":  {"v1", map[string]Rates{"a": {CacheHitPerMillion: ptr(-1)}}},
	}
	for name, tc := range cases {
		if _, err := NewTable(tc.version, tc.prices); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestTableVersionIsExposed(t *testing.T) {
	if got := testTable(t).Version; got != "2026-02-01" {
		t.Fatalf("version = %q", got)
	}
	if _, ok := testTable(t).Price("murah"); !ok {
		t.Fatal("known model not found")
	}
	if _, ok := testTable(t).Price("lain"); ok {
		t.Fatal("unknown model found")
	}
}
