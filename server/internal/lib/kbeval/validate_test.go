package kbeval

import (
	"bytes"
	"math"
	"strings"
	"testing"
)

func TestInvalidInputs(t *testing.T) {
	tests := map[string]func(*Fixture, *Rankings){
		"missing scope":       func(f *Fixture, r *Rankings) { f.Scope = "" },
		"invalid scope":       func(f *Fixture, r *Rankings) { f.Scope = "tenant:bad" },
		"cross tenant chunk":  func(f *Fixture, r *Rankings) { f.Chunks[0].Scope = "tenant:11111111-1111-1111-1111-111111111111" },
		"missing chunk scope": func(f *Fixture, r *Rankings) { f.Chunks[0].Scope = "" },
		"duplicate query":     func(f *Fixture, r *Rankings) { f.Queries = append(f.Queries, f.Queries[0]) },
		"duplicate chunk":     func(f *Fixture, r *Rankings) { f.Chunks = append(f.Chunks, f.Chunks[0]) },
		"duplicate label": func(f *Fixture, r *Rankings) {
			f.Queries[0].Labels = append(f.Queries[0].Labels, f.Queries[0].Labels[0])
		},
		"unknown label":        func(f *Fixture, r *Rankings) { f.Queries[0].Labels[0].ChunkID = "missing" },
		"NaN":                  func(f *Fixture, r *Rankings) { f.Queries[0].Labels[0].Grade = math.NaN() },
		"infinity":             func(f *Fixture, r *Rankings) { f.Queries[0].Labels[0].Grade = math.Inf(1) },
		"negative grade":       func(f *Fixture, r *Rankings) { f.Queries[0].Labels[0].Grade = -1 },
		"large grade":          func(f *Fixture, r *Rankings) { f.Queries[0].Labels[0].Grade = 4 },
		"duplicate result":     func(f *Fixture, r *Rankings) { r.Results = append(r.Results, r.Results[0]) },
		"duplicate candidate":  func(f *Fixture, r *Rankings) { r.Results[0].Candidates[0].ChunkID = "a" },
		"duplicate rank":       func(f *Fixture, r *Rankings) { r.Results[0].Candidates[0].Rank = 2 },
		"zero rank":            func(f *Fixture, r *Rankings) { r.Results[0].Candidates[0].Rank = 0 },
		"negative rank":        func(f *Fixture, r *Rankings) { r.Results[0].Candidates[0].Rank = -1 },
		"gap rank":             func(f *Fixture, r *Rankings) { r.Results[0].Candidates[0].Rank = 3 },
		"unknown candidate":    func(f *Fixture, r *Rankings) { r.Results[0].Candidates[0].ChunkID = "unknown" },
		"missing decision":     func(f *Fixture, r *Rankings) { r.Results[0].Answered = nil },
		"missing query":        func(f *Fixture, r *Rankings) { r.Results = r.Results[1:] },
		"wrong split":          func(f *Fixture, r *Rankings) { r.Results[0].QueryID = "h" },
		"unknown split":        func(f *Fixture, r *Rankings) { r.Split = "test" },
		"missing heldout":      func(f *Fixture, r *Rankings) { f.Queries = f.Queries[:3] },
		"cross split text":     func(f *Fixture, r *Rankings) { f.Queries[3].Text = "  ANSWERABLE  " },
		"contradiction":        func(f *Fixture, r *Rankings) { f.Queries[0].NoAnswer = true },
		"unlabeled answerable": func(f *Fixture, r *Rankings) { f.Queries[0].Labels = nil },
		"wrong fixture":        func(f *Fixture, r *Rankings) { r.FixtureID = "wrong" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			f, r := sample()
			mutate(&f, &r)
			if _, err := Evaluate(f, r); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}
func TestStrictDecode(t *testing.T) {
	for _, input := range []string{`{"unknown":1}`, `{} {}`, `{"queries":[{"labels":[{"grade":1e999}]}]}`} {
		var f Fixture
		if Decode(strings.NewReader(input), &f) == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}
func TestOrderIndependentReport(t *testing.T) {
	f, r := sample()
	a, err := Evaluate(f, r)
	if err != nil {
		t.Fatal(err)
	}
	r.Results[0].Candidates[0], r.Results[0].Candidates[1] = r.Results[0].Candidates[1], r.Results[0].Candidates[0]
	r.Results[0], r.Results[2] = r.Results[2], r.Results[0]
	b, err := Evaluate(f, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, encode := range []func(*bytes.Buffer, Report) error{func(w *bytes.Buffer, r Report) error { return WriteJSON(w, r) }, func(w *bytes.Buffer, r Report) error { return WriteCSV(w, r) }} {
		var x, y bytes.Buffer
		if err := encode(&x, a); err != nil {
			t.Fatal(err)
		}
		if err := encode(&y, b); err != nil {
			t.Fatal(err)
		}
		if x.String() != y.String() {
			t.Fatal("report depends on input order")
		}
	}
}
