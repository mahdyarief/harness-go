package kb

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

func TestWeightedRRF(t *testing.T) {
	a := Candidate{ChunkID: "a", DocumentID: "a", Content: "alpha"}
	b := Candidate{ChunkID: "b", DocumentID: "b", Content: "beta"}
	for _, tc := range []struct {
		name              string
		lexical, semantic []Candidate
		lw, sw            float64
		want              string
		score             float64
	}{
		{"one based", []Candidate{a}, nil, 1, 1, "a", 1.0 / 61},
		{"fused", []Candidate{a, b}, []Candidate{b, a}, 1, 2, "b", 1.0/62 + 2.0/61},
		{"duplicate branch", []Candidate{a, a}, []Candidate{b}, 1, 1, "a", 1.0 / 61},
		{"invalid weight", []Candidate{a}, []Candidate{b}, math.NaN(), 1, "b", 1.0 / 61},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := WeightedRRF(tc.lexical, tc.semantic, tc.lw, tc.sw)
			if len(got) == 0 || got[0].ChunkID != tc.want || math.Abs(got[0].Score-tc.score) > 1e-12 {
				t.Fatalf("got %+v", got)
			}
			if again := WeightedRRF(tc.lexical, tc.semantic, tc.lw, tc.sw); !reflect.DeepEqual(got, again) {
				t.Fatal("unstable fusion")
			}
		})
	}
	var lexical, semantic []Candidate
	for i := 0; i < 70; i++ {
		lexical = append(lexical, Candidate{ChunkID: fmt.Sprintf("l%d", i)})
		semantic = append(semantic, Candidate{ChunkID: fmt.Sprintf("s%d", i)})
	}
	if got := WeightedRRF(lexical, semantic, 1, 1); len(got) != 100 {
		t.Fatalf("union=%d", len(got))
	}
}

func TestSelectCandidates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input []Candidate
		want  int
	}{
		{"empty", nil, 0},
		{"hash dedup", []Candidate{{ChunkID: "1", DocumentID: "a", Content: "same"}, {ChunkID: "2", DocumentID: "b", Content: "same"}}, 1},
		{"soft two per document", []Candidate{{ChunkID: "1", DocumentID: "a", Content: "alpha"}, {ChunkID: "2", DocumentID: "a", Content: "beta"}, {ChunkID: "3", DocumentID: "a", Content: "gamma"}, {ChunkID: "4", DocumentID: "b", Content: "delta"}}, 4},
		{"persisted hash dedup", []Candidate{{ChunkID: "1", DocumentID: "a", Content: "alpha", ContentHash: "same"}, {ChunkID: "2", DocumentID: "b", Content: "beta", ContentHash: "same"}}, 1},
		{"chunk dedup", []Candidate{{ChunkID: "1", Content: "alpha"}, {ChunkID: "1", Content: "beta"}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SelectCandidates(tc.input, 5); len(got) != tc.want {
				t.Fatalf("got %+v", got)
			}
		})
	}
	if got := SelectCandidates([]Candidate{{Content: "a"}}, 0); len(got) != 0 {
		t.Fatal("zero limit ignored")
	}
}

func TestSelectCandidatesSoftCapStableRank(t *testing.T) {
	ranked := []Candidate{
		{ChunkID: "1", DocumentID: "a", Content: "alpha"},
		{ChunkID: "2", DocumentID: "a", Content: "beta"},
		{ChunkID: "3", DocumentID: "a", Content: "gamma"},
		{ChunkID: "4", DocumentID: "a", Content: "delta"},
		{ChunkID: "5", DocumentID: "a", Content: "epsilon"},
		{ChunkID: "duplicate", DocumentID: "a", Content: "gamma"},
	}
	for limit := 3; limit <= MaxPassages; limit++ {
		got := SelectCandidates(ranked, limit)
		if len(got) != limit {
			t.Fatalf("single document limit=%d: %+v", limit, got)
		}
		for i := range got {
			if got[i].ChunkID != ranked[i].ChunkID {
				t.Fatalf("single document rank changed: %+v", got)
			}
		}
	}
	ranked = append(ranked[:4:4], Candidate{ChunkID: "other", DocumentID: "b", Content: "zeta"})
	for _, tc := range []struct {
		limit int
		want  []string
	}{
		{3, []string{"1", "2", "other"}},
		{4, []string{"1", "2", "3", "other"}},
		{5, []string{"1", "2", "3", "4", "other"}},
	} {
		got := SelectCandidates(ranked, tc.limit)
		if len(got) != len(tc.want) {
			t.Fatalf("limit=%d: %+v", tc.limit, got)
		}
		for i, id := range tc.want {
			if got[i].ChunkID != id {
				t.Fatalf("limit=%d: expected %v, got %+v", tc.limit, tc.want, got)
			}
		}
	}
}
