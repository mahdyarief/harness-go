package rules

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var claimSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"amount":      {"type": "number"},
		"kind":        {"type": "string"},
		"tags":        {"type": "array", "items": {"type": "string"}},
		"approved":    {"type": "boolean"},
		"submitted_at": {"type": "string"},
		"reviewer":    {"type": "object", "properties": {"name": {"type": "string"}, "level": {"type": "number"}, "title": {"type": "string"}}}
	},
	"required": ["amount", "kind"]
}`)

func mustLoad(t *testing.T, rules ...Rule) *Engine {
	t.Helper()
	engine, err := Load(claimSchema, rules)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return engine
}

func eval(t *testing.T, e *Engine, subject string, group string, at time.Time) []Hit {
	t.Helper()
	hits := e.Evaluate(json.RawMessage(subject), group, at)
	if hits == nil {
		t.Fatalf("evaluate returned nil")
	}
	return hits
}

func TestEveryOperator(t *testing.T) {
	at := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	subject := `{"amount": 5000, "kind": "medical", "tags": ["a", "b"], "approved": true, "submitted_at": "2026-06-01", "reviewer": {"name": "andi", "level": 3}}`
	cases := []struct {
		expr string
		want bool
	}{
		{"amount > 4000", true},
		{"amount >= 5000", true},
		{"amount < 4000", false},
		{"amount <= 4999", false},
		{"amount == 5000", true},
		{"amount != 5000", false},
		{"kind == \"medical\"", true},
		{"kind != \"medical\"", false},
		{"kind in [\"medical\", \"dental\"]", true},
		{"kind in [\"x\", \"y\"]", false},
		{"\"a\" in kind", true},
		{"\"z\" in kind", false},
		{"\"edi\" in kind", true},
		{"approved", true},
		{"not approved", false},
		{"not not approved", true},
		{"approved and amount > 100", true},
		{"approved and amount > 99999", false},
		{"approved or amount > 99999", true},
		{"not approved or amount > 99999", false},
		{"len(tags) == 2", true},
		{"len(tags) > 5", false},
		{"len(\"medical\") == 7", true},
		{"exists(reviewer.name)", true},
		{"exists(reviewer.title)", false},
		{"between(amount, 1000, 6000)", true},
		{"between(amount, 1000, 4000)", false},
		{"submitted_at == date(\"2026-06-01\")", true},
		{"(date(\"2026-06-15\") - date(\"2026-06-01\")) <= 30", true},
		{"(date(\"2026-06-15\") - date(\"2026-06-01\")) <= 10", false},
		{"date(\"2026-06-01\") + 14 == date(\"2026-06-15\")", true},
		{"amount + 1 == 5001", true},
		{"amount - 1000 > 3000", true},
		{"reviewer.level == 3", true},
		{"reviewer.name == \"andi\" and reviewer.level > 2", true},
	}
	for i, c := range cases {
		engine := mustLoad(t, Rule{ID: "op", Version: "1", Group: "g", When: c.expr, Message: "m"})
		got := eval(t, engine, subject, "g", at)
		if len(got) != 1 {
			t.Fatalf("case %d (%s): got %d hits", i, c.expr, len(got))
		}
		if got[0].Fired != c.want {
			t.Fatalf("case %d: %s fired = %v; want %v", i, c.expr, got[0].Fired, c.want)
		}
	}
}

func TestUnknownFieldRejectedAtLoad(t *testing.T) {
	_, err := Load(claimSchema, []Rule{{ID: "bad", Version: "1", Group: "g", When: "no_such_field > 1"}})
	if err == nil {
		t.Fatal("want load error for unknown field")
	}
	if !strings.Contains(err.Error(), "no_such_field") {
		t.Fatalf("error = %v; must name the field", err)
	}
}

func TestInvalidRuleIsALoadErrorWithLine(t *testing.T) {
	rules := []Rule{
		{ID: "one", Version: "1", Group: "g", When: "amount > 1"},
		{ID: "two", Version: "1", Group: "g", When: "amount >"}, // incomplete
	}
	_, err := Load(claimSchema, rules)
	if err == nil {
		t.Fatal("want load error for incomplete expression")
	}
	if !strings.Contains(err.Error(), "two") {
		t.Fatalf("error = %v; must name the rule", err)
	}
}

func TestLoadYAMLReportsLine(t *testing.T) {
	doc := `
rules:
  - id: good
    version: "1"
    group: g
    when: amount > 1
  - id: bad
    version: "1"
    group: g
    when: ghost_field > 1
`
	rules, err := LoadYAML(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Load(claimSchema, rules)
	if err == nil {
		t.Fatal("want load error from bad rule")
	}
	if !strings.Contains(err.Error(), "baris 7") {
		t.Fatalf("error = %v; must carry the source line", err)
	}
}

func TestEffectiveDates(t *testing.T) {
	fromV1, toV1 := "2026-01-01", "2026-05-31"
	fromV2 := "2026-06-01"
	v1 := "amount > 100"
	v2 := "amount > 1000000" // a v2 rule that never fires for 5000
	engine := mustLoad(t,
		Rule{ID: "high", Version: "1", Group: "g", When: v1, Effective: &Effective{From: &fromV1, To: &toV1}},
		Rule{ID: "high", Version: "2", Group: "g", When: v2, Effective: &Effective{From: &fromV2}},
	)
	atMid := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	hits := eval(t, engine, `{"amount": 5000}`, "g", atMid)
	if len(hits) != 1 || hits[0].Version != "1" || !hits[0].Fired {
		t.Fatalf("mid-window: %+v", hits)
	}
	atLater := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	hits = eval(t, engine, `{"amount": 5000}`, "g", atLater)
	if len(hits) != 1 || hits[0].Version != "2" || hits[0].Fired {
		t.Fatalf("post-window: %+v", hits)
	}
}

func TestDeterminism(t *testing.T) {
	at := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	subject := `{"amount": 5000, "kind": "medical", "tags": ["b", "a"], "approved": true, "submitted_at": "2026-06-01"}`
	engine := mustLoad(t,
		Rule{ID: "r1", Version: "1", Group: "g", When: "amount > 1000", Message: "nilai {{.amount}}", Source: "PER-2/2024"},
		Rule{ID: "r2", Version: "1", Group: "g", When: "kind == \"medical\" and approved"},
	)
	a, err := json.Marshal(eval(t, engine, subject, "g", at))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(eval(t, engine, subject, "g", at))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("outputs differ:\n%s\n%s", a, b)
	}
}

func TestHitEvidenceCitesRuleAndFields(t *testing.T) {
	at := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	engine := mustLoad(t, Rule{
		ID: "big", Version: "3", Group: "g", When: "amount > 4000",
		Message: "lebih dari batas", Source: "PER-7/2026 pasal 12",
	})
	hits := eval(t, engine, `{"amount": 9000, "kind": "medical"}`, "g", at)
	if len(hits) != 1 || !hits[0].Fired {
		t.Fatalf("hits = %+v", hits)
	}
	var ruleCited, fieldCited bool
	for _, ev := range hits[0].Evidence {
		if ev.Kind == "RULE" && ev.Ref == "big@3" {
			ruleCited = true
			if ev.Meta["source"] != "PER-7/2026 pasal 12" {
				t.Fatalf("meta = %+v", ev.Meta)
			}
		}
		if ev.Kind == "FIELD" && ev.Ref == "amount" && ev.Excerpt == "9000" {
			fieldCited = true
		}
	}
	if !ruleCited || !fieldCited {
		t.Fatalf("evidence = %+v; must cite the rule version and the read fields", hits[0].Evidence)
	}
}

func TestToolReturnsHitsWithoutLLM(t *testing.T) {
	engine := mustLoad(t, Rule{ID: "big", Version: "1", Group: "g", When: "amount > 4000"})
	at := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	tool := NewTool(engine, func() time.Time { return at })
	out, err := tool.Execute(t.Context(), json.RawMessage(`{"subject":{"amount":9000,"kind":"x"},"group":"g"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, `"fired":true`) || !strings.Contains(out.Output, "big") {
		t.Fatalf("output = %s", out.Output)
	}
	if len(out.Evidence) == 0 {
		t.Fatal("evidence must be attached to the result")
	}
	if _, err := tool.Execute(t.Context(), json.RawMessage(`{"subject":{},"group":"missing"}`)); err == nil {
		t.Fatal("an unknown group must error, not look clean")
	}
	if out, err := tool.Execute(t.Context(), json.RawMessage(`{"subject":{"amount":1},"group":"g"}`)); err != nil {
		t.Fatal(err)
	} else if strings.Contains(out.Output, `"fired":true`) {
		t.Fatalf("output = %s", out.Output)
	}
}
