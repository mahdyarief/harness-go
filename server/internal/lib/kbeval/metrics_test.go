package kbeval

import (
	"fmt"
	"math"
	"testing"
)

func sample() (Fixture, Rankings) {
	yes, no := true, false
	f := Fixture{Scope: "global", ID: "test", Provenance: "independent hand-calculated test", Chunks: []Chunk{{"a", "d1", "A", "global"}, {"b", "d2", "B", "global"}, {"x", "d0", "X", "global"}}, Queries: []Query{
		{ID: "q", Split: "dev", Category: "multi", Text: "answerable", Labels: []Label{{"a", 3}, {"b", 1}}},
		{ID: "n1", Split: "dev", Category: "none", Text: "unknown one", NoAnswer: true},
		{ID: "n2", Split: "dev", Category: "none", Text: "unknown two", NoAnswer: true},
		{ID: "h", Split: "heldout", Category: "multi", Text: "heldout question", Labels: []Label{{"a", 3}}},
	}}
	r := Rankings{FixtureID: "test", RunID: "r", Split: "dev", Provenance: "test", Results: []Ranking{{"q", &yes, []Candidate{{"x", 1}, {"a", 2}}}, {"n1", &yes, nil}, {"n2", &no, []Candidate{{"a", 1}}}}}
	return f, r
}
func closeTo(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-12 {
		t.Fatalf("got %v, want %.15g", got, want)
	}
}
func TestHandCalculatedMetrics(t *testing.T) {
	f, r := sample()
	report, err := Evaluate(f, r)
	if err != nil {
		t.Fatal(err)
	}
	m := report.Summary
	closeTo(t, m.Recall20, 0.5)
	closeTo(t, m.Recall50, 0.5)
	closeTo(t, m.MRR5, 0.5)
	// DCG = 7/log2(3); ideal = 7 + 1/log2(3), derived independently.
	closeTo(t, m.NDCG5, (7/math.Log2(3))/(7+1/math.Log2(3)))
	closeTo(t, m.SourceCoverage5, 0.5)
	closeTo(t, m.NoAnswerFalsePositive, 0.5)
	if m.Queries != 3 || m.Answerable != 1 || m.NoAnswer != 2 || m.MultiSource != 1 {
		t.Fatalf("counts: %+v", m)
	}
}
func TestRankCutoffs(t *testing.T) {
	for _, rank := range []int{1, 5, 6, 20, 21, 50, 51} {
		t.Run(fmt.Sprint(rank), func(t *testing.T) {
			f, r := sample()
			f.Queries[0].Labels = []Label{{"a", 3}}
			candidates := []Candidate{}
			for i := 1; i < rank; i++ {
				id := fmt.Sprintf("f%d", i)
				f.Chunks = append(f.Chunks, Chunk{id, "filler", id, "global"})
				candidates = append(candidates, Candidate{id, i})
			}
			r.Results[0].Candidates = append(candidates, Candidate{"a", rank})
			report, err := Evaluate(f, r)
			if err != nil {
				t.Fatal(err)
			}
			m := report.Summary
			recall20, recall50, rr, ndcg := 0.0, 0.0, 0.0, 0.0
			if rank <= 20 {
				recall20 = 1
			}
			if rank <= 50 {
				recall50 = 1
			}
			if rank <= 5 {
				rr = 1 / float64(rank)
				ndcg = 1 / math.Log2(float64(rank+1))
			}
			closeTo(t, m.Recall20, recall20)
			closeTo(t, m.Recall50, recall50)
			closeTo(t, m.MRR5, rr)
			closeTo(t, m.NDCG5, ndcg)
			if m.SourceCoverage5 != nil {
				t.Fatal("single-document coverage must be null")
			}
		})
	}
}
