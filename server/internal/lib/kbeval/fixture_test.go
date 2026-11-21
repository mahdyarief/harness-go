package kbeval

import (
	"os"
	"strings"
	"testing"
)

func TestSyntheticFixture(t *testing.T) {
	file, err := os.Open("testdata/synthetic-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var f Fixture
	if err := Decode(file, &f); err != nil {
		t.Fatal(err)
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(f.Queries) != 150 || len(f.Chunks) != 60 {
		t.Fatalf("unexpected sizes %d/%d", len(f.Queries), len(f.Chunks))
	}
	if !strings.Contains(f.Provenance, "machine-generated") || !strings.Contains(f.Provenance, "NOT human ground truth") {
		t.Fatal("synthetic provenance missing")
	}
	counts := map[string]int{}
	categories := map[string]bool{}
	for _, q := range f.Queries {
		counts[q.Split]++
		categories[q.Category] = true
		if q.NoAnswer {
			counts["no_answer"]++
		}
	}
	if counts["dev"] != 105 || counts["heldout"] != 45 || counts["no_answer"] != 30 || len(categories) != 7 {
		t.Fatalf("fixture composition: %v %v", counts, categories)
	}
	for _, split := range []string{"dev", "heldout"} {
		file, err := os.Open("testdata/illustrative-" + split + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var r Rankings
		err = Decode(file, &r)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(r.Provenance, "NOT SQL baseline") {
			t.Fatal("illustrative run must not masquerade as baseline")
		}
		report, err := Evaluate(f, r)
		if err != nil {
			t.Fatal(err)
		}
		if report.Summary.Queries != counts[split] {
			t.Fatal("missing results")
		}
		measured, err := os.Open("testdata/sql-baseline-" + split + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var baseline Rankings
		err = Decode(measured, &baseline)
		measured.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(baseline.Provenance, "Actual local PostgreSQL") {
			t.Fatal("missing measured provenance")
		}
		actual, err := Evaluate(f, baseline)
		if err != nil {
			t.Fatal(err)
		}
		if actual.Summary.Queries != counts[split] {
			t.Fatal("incomplete baseline")
		}
	}
}

func TestEmptyCandidatesAndNoAnswerOnly(t *testing.T) {
	f, r := sample()
	r.Results[0].Candidates = nil
	report, err := Evaluate(f, r)
	if err != nil {
		t.Fatal(err)
	}
	closeTo(t, report.Summary.Recall20, 0)
	closeTo(t, report.Summary.NDCG5, 0)
	closeTo(t, report.Summary.MRR5, 0)
	f.Queries = append(f.Queries[1:3], f.Queries[3])
	r.Results = r.Results[1:]
	report, err = Evaluate(f, r)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Recall20 != nil || report.Summary.NDCG5 != nil {
		t.Fatal("undefined metrics must be null")
	}
	closeTo(t, report.Summary.NoAnswerFalsePositive, 0.5)
}
