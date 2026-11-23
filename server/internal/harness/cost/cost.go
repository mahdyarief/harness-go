// Package cost converts billed token usage into USD using a versioned price
// table. The table is data, supplied by the consumer and pinned by version, so
// an archived run can always be priced with the rates that were in force when
// it ran.
package cost

import (
	"errors"
	"fmt"
	"strings"
)

// Rates are prices in USD per one million tokens. CacheHitPerMillion is
// optional: when nil, cached prompt tokens are billed at InputPerMillion.
type Rates struct {
	InputPerMillion    float64  `json:"input_per_million" yaml:"input_per_million"`
	OutputPerMillion   float64  `json:"output_per_million" yaml:"output_per_million"`
	CacheHitPerMillion *float64 `json:"cache_hit_per_million,omitempty" yaml:"cache_hit_per_million,omitempty"`
}

// Usage is billed token usage. CacheHitTokens is a subset of PromptTokens.
// Callers without a cache split leave it zero.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	CacheHitTokens   int
}

// Table maps a model name to its rates. Version identifies the rate card so a
// stored cost can name the prices that produced it.
type Table struct {
	Version string           `json:"version" yaml:"version"`
	Prices  map[string]Rates `json:"prices" yaml:"prices"`
}

// NewTable validates and copies a rate card. It rejects a table without a
// version or model and any negative rate, so a typo cannot silently zero out
// spend.
func NewTable(version string, prices map[string]Rates) (Table, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return Table{}, errors.New("cost: versi tabel harga wajib")
	}
	if len(prices) == 0 {
		return Table{}, errors.New("cost: tabel harga kosong")
	}
	copied := make(map[string]Rates, len(prices))
	for model, rates := range prices {
		name := strings.TrimSpace(model)
		if name == "" {
			return Table{}, errors.New("cost: nama model kosong")
		}
		if rates.InputPerMillion < 0 || rates.OutputPerMillion < 0 {
			return Table{}, fmt.Errorf("cost: harga negatif untuk model %q", name)
		}
		if rates.CacheHitPerMillion != nil && *rates.CacheHitPerMillion < 0 {
			return Table{}, fmt.Errorf("cost: harga cache negatif untuk model %q", name)
		}
		copied[name] = rates
	}
	return Table{Version: version, Prices: copied}, nil
}

// Price returns the rates for model, if the table knows it.
func (t Table) Price(model string) (Rates, bool) {
	rates, ok := t.Prices[model]
	return rates, ok
}

// UnknownModelError marks usage the table cannot price. It is a warning, never
// a failure: an unpriced model must not abort a run.
type UnknownModelError struct {
	Model        string
	TableVersion string
}

func (e *UnknownModelError) Error() string {
	return fmt.Sprintf("cost: model %q tidak ada di tabel %q", e.Model, e.TableVersion)
}

// Amount is a computed cost. Known is false when the model was not priced, so
// callers can distinguish a real zero from "unknown".
type Amount struct {
	USD   float64
	Known bool
}

// IsUnknown reports whether err means the model is missing from the table.
func IsUnknown(err error) bool {
	var unknown *UnknownModelError
	return errors.As(err, &unknown)
}

// Cost prices one usage. Prompt tokens above CacheHitTokens bill at the input
// rate; the cached remainder bills at the cache rate when one is configured.
// Negative token counts are clamped to zero.
func (t Table) Cost(model string, usage Usage) (Amount, error) {
	rates, ok := t.Prices[model]
	if !ok {
		return Amount{}, &UnknownModelError{Model: model, TableVersion: t.Version}
	}
	prompt := max(0, usage.PromptTokens)
	completion := max(0, usage.CompletionTokens)
	cached := min(max(0, usage.CacheHitTokens), prompt)
	usd := float64(prompt-cached)/1e6*rates.InputPerMillion +
		float64(completion)/1e6*rates.OutputPerMillion
	if cached > 0 {
		rate := rates.InputPerMillion
		if rates.CacheHitPerMillion != nil {
			rate = *rates.CacheHitPerMillion
		}
		usd += float64(cached) / 1e6 * rate
	}
	return Amount{USD: usd, Known: true}, nil
}
