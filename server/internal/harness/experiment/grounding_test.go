package experiment

import (
	"context"
	"encoding/json"
	"testing"

	"hsg/internal/harness/evidence"
	"hsg/internal/harness/review"
)

func TestGroundingFromEvidenceKinds(t *testing.T) {
	ds := Dataset{Items: []Item{
		{ID: "1", Input: json.RawMessage(`{}`)},
		{ID: "2", Input: json.RawMessage(`{}`)},
	}}
	var seen int
	arm := Arm{Name: "base", Run: func(context.Context, json.RawMessage) (ArmResult, error) {
		seen++
		if seen == 1 {
			return ArmResult{
				Output: json.RawMessage(`{"cites":["doc-1"]}`),
				Stats:  RunStats{Evidence: []evidence.Item{{Kind: "DOCUMENT", Ref: "doc-1"}}},
			}, nil
		}
		return ArmResult{Output: json.RawMessage(`{"cites":["doc-2"]}`)}, nil
	}}
	res, err := Execute(context.Background(), ds, []Arm{arm}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	report := BuildReport(res, ds, MetricsConfig{
		EvidenceKinds: []string{"DOCUMENT"},
		CitationPath:  "cites",
	})
	g := report.Arms[0].Grounding
	if !g.Measured || g.Items != 2 || g.Grounded != 1 || g.Rate == nil || *g.Rate != 0.5 {
		t.Fatalf("grounding = %+v", g)
	}
}

func TestGroundingWithoutCitationPathAcceptsAnyEvidence(t *testing.T) {
	ds := Dataset{Items: []Item{{ID: "1", Input: json.RawMessage(`{}`)}}}
	arm := Arm{Name: "base", Run: func(context.Context, json.RawMessage) (ArmResult, error) {
		return ArmResult{
			Output: json.RawMessage(`{"answer":"x"}`),
			Stats:  RunStats{Evidence: []evidence.Item{{Kind: "RULE", Ref: "r@1"}}},
		}, nil
	}}
	res, err := Execute(context.Background(), ds, []Arm{arm}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	g := BuildReport(res, ds, MetricsConfig{EvidenceKinds: []string{"RULE"}}).Arms[0].Grounding
	if !g.Measured || g.Grounded != 1 || g.Rate == nil || *g.Rate != 1 {
		t.Fatalf("grounding = %+v", g)
	}
}

func TestReviewAgreementUsesDecidedResult(t *testing.T) {
	store := review.NewMemory()
	ctx := context.Background()
	items := []review.Item{
		{RunID: "run-1", Subject: "1", Result: json.RawMessage(`{"label":"A"}`), Reason: "ragu", Role: "reviewer"},
		{RunID: "run-2", Subject: "2", Result: json.RawMessage(`{"label":"A"}`), Reason: "ragu", Role: "reviewer"},
	}
	decisions := []string{`{"label":"A"}`, `{"label":"B"}`}
	for i := range items {
		created, err := store.Create(ctx, items[i])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Decide(ctx, created.ID, review.Decision{
			Reviewer: "r", Role: "reviewer", Result: json.RawMessage(decisions[i]),
		}); err != nil {
			t.Fatal(err)
		}
	}
	final, err := DecidedResults(ctx, store, 10)
	if err != nil {
		t.Fatal(err)
	}
	agree := ReviewAgreement(final, nil)
	if same, ok := agree("1", json.RawMessage(`{"label":"A"}`), nil); !ok || !same {
		t.Fatalf("item 1: same=%v ok=%v", same, ok)
	}
	if same, ok := agree("2", json.RawMessage(`{"label":"A"}`), nil); !ok || same {
		t.Fatalf("item 2: same=%v ok=%v", same, ok)
	}
	if _, ok := agree("3", json.RawMessage(`{}`), nil); ok {
		t.Fatal("item tanpa keputusan harus dilewati")
	}

	ds := Dataset{Items: []Item{
		{ID: "1", Input: json.RawMessage(`{}`)},
		{ID: "2", Input: json.RawMessage(`{}`)},
	}}
	arm := Arm{Name: "base", Run: func(context.Context, json.RawMessage) (ArmResult, error) {
		return ArmResult{Output: json.RawMessage(`{"label":"A"}`)}, nil
	}}
	res, err := Execute(ctx, ds, []Arm{arm}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	a := BuildReport(res, ds, MetricsConfig{AgreeItem: agree}).Arms[0].Agreement
	if !a.Measured || a.Items != 2 || a.Agreed != 1 || a.Rate == nil || *a.Rate != 0.5 {
		t.Fatalf("agreement = %+v", a)
	}
}
