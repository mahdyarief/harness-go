package kbeval

import (
	"math"
	"testing"
)

func TestMacroMeanAndDocumentDeduplication(t *testing.T) {
	f, r := sample()
	f.Chunks = append(f.Chunks, Chunk{"a2", "d1", "another chunk of same document", "global"})
	f.Queries[0].Labels = append(f.Queries[0].Labels, Label{"a2", 2})
	r.Results[0].Candidates = []Candidate{{"a", 1}, {"a2", 2}}
	f.Queries = append(f.Queries, Query{ID: "q2", Split: "dev", Category: "single", Text: "second answerable", Labels: []Label{{"b", 3}}})
	yes := true
	r.Results = append(r.Results, Ranking{"q2", &yes, nil})
	report, err := Evaluate(f, r)
	if err != nil {
		t.Fatal(err)
	}
	// Per-query recall 2/3 and 0 => macro 1/3, not micro 2/4.
	closeTo(t, report.Summary.Recall20, 1.0/3)
	closeTo(t, report.Summary.MRR5, 0.5)
	// Two relevant chunks from d1 still cover only one of two documents.
	closeTo(t, report.Summary.SourceCoverage5, 0.5)
	// Ideal includes the missing grade-1 chunk at rank 3.
	want := (7 + 3/math.Log2(3)) / (7 + 3/math.Log2(3) + 1/math.Log2(4))
	closeTo(t, report.Summary.NDCG5, want/2)
}
