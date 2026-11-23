package experiment

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"hsg/internal/harness/rules"
)

const fixtureJSONL = `{"id":"1","split":"dev","input":{"i":0},"label":{"label":"A"}}
{"id":"2","split":"dev","input":{"i":1},"label":{"label":"A"}}
{"id":"3","split":"dev","input":{"i":2},"label":{"label":"B"}}
{"id":"4","split":"heldout","input":{"i":3},"label":{"label":"B"}}
{"id":"5","split":"heldout","input":{"i":4},"label":{"label":"A"}}
`

func fixtureDataset(t *testing.T) Dataset {
	t.Helper()
	ds, err := DecodeDataset(strings.NewReader(fixtureJSONL))
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func fakeArm(name string, predictions []string) Arm {
	return Arm{Name: name, Run: func(_ context.Context, input json.RawMessage) (ArmResult, error) {
		var in struct {
			I int `json:"i"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return ArmResult{}, err
		}
		out, err := json.Marshal(map[string]any{"label": predictions[in.I]})
		if err != nil {
			return ArmResult{}, err
		}
		return ArmResult{Output: out, Stats: RunStats{
			Latency:          time.Duration(in.I+1) * 10 * time.Millisecond,
			PromptTokens:     10,
			CompletionTokens: 5,
			Cost:             ptr(float64(in.I+1) * 0.001),
		}}, nil
	}}
}

func TestDatasetValidation(t *testing.T) {
	cases := map[string]string{
		"duplicate id":  "{\"id\":\"a\",\"input\":{}}\n{\"id\":\"a\",\"input\":{}}\n",
		"empty id":      "{\"id\":\"\",\"input\":{}}\n",
		"blank input":   "{\"id\":\"a\"}\n",
		"invalid input": "{\"id\":\"a\",\"input\":not-json}\n",
		"invalid label": "{\"id\":\"a\",\"input\":{},\"label\":nope}\n",
		"empty":         "\n\n",
	}
	for name, body := range cases {
		if _, err := DecodeDataset(strings.NewReader(body)); err == nil {
			t.Fatalf("%s: harus ditolak", name)
		}
	}
	ds, err := DecodeDataset(strings.NewReader("\n" + fixtureJSONL))
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Items) != 5 || ds.Items[4].Split != "heldout" {
		t.Fatalf("items = %+v", ds.Items)
	}
}

func TestClassificationMatchesHandCalculation(t *testing.T) {
	ds := fixtureDataset(t)
	arms := []Arm{
		fakeArm("base", []string{"A", "B", "B", "B", "A"}),
		fakeArm("all-a", []string{"A", "A", "A", "A", "A"}),
	}
	res, err := Execute(context.Background(), ds, arms, Config{Parallel: 3})
	if err != nil {
		t.Fatal(err)
	}
	metrics := BuildReport(res, ds, MetricsConfig{LabelPath: "label"}).Arms
	base, allA := metrics[0], metrics[1]

	assertScore(t, "base micro", base.Classification.Micro, 0.8, 0.8, 0.8)
	assertScore(t, "base macro", base.Classification.Macro, 5.0/6.0, 5.0/6.0, 0.8)
	assertScore(t, "all-a micro", allA.Classification.Micro, 0.6, 0.6, 0.6)
	assertScore(t, "all-a macro", allA.Classification.Macro, 0.3, 0.5, 0.375)
	if got := base.Classification.Confusion[0]; got.Actual != "A" || got.Predicted["A"] != 2 || got.Predicted["B"] != 1 {
		t.Fatalf("confusion A = %+v", got)
	}
	if got := base.Classification.Confusion[1]; got.Actual != "B" || got.Predicted["B"] != 2 || len(got.Predicted) != 1 {
		t.Fatalf("confusion B = %+v", got)
	}
	if base.Latency.Samples != 5 || *base.Latency.P50MS != 30 || *base.Latency.P95MS != 50 {
		t.Fatalf("latency = %+v", base.Latency)
	}
	if base.Usage.PromptTokens != 50 || base.Usage.CompletionTokens != 25 || base.Usage.TotalTokens != 75 {
		t.Fatalf("usage = %+v", base.Usage)
	}
	if base.Usage.CostUSD == nil || math.Abs(*base.Usage.CostUSD-0.015) > 1e-12 {
		t.Fatalf("cost = %v", base.Usage.CostUSD)
	}
}

func assertScore(t *testing.T, label string, got Score, p, r, f float64) {
	t.Helper()
	if got.Precision == nil || got.Recall == nil || got.F1 == nil {
		t.Fatalf("%s: score nil = %+v", label, got)
	}
	for name, pair := range map[string][2]float64{"precision": {*got.Precision, p}, "recall": {*got.Recall, r}, "f1": {*got.F1, f}} {
		if math.Abs(pair[0]-pair[1]) > 1e-9 {
			t.Fatalf("%s %s = %v; want %v", label, name, pair[0], pair[1])
		}
	}
}

func TestReportIsByteIdenticalAcrossParallelRuns(t *testing.T) {
	ds := fixtureDataset(t)
	arms := func() []Arm {
		return []Arm{
			fakeArm("base", []string{"A", "B", "B", "B", "A"}),
			fakeArm("all-a", []string{"A", "A", "A", "A", "A"}),
		}
	}
	first, err := Execute(context.Background(), ds, arms(), Config{Parallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Execute(context.Background(), ds, arms(), Config{Parallel: 5})
	if err != nil {
		t.Fatal(err)
	}
	cfg := MetricsConfig{LabelPath: "label"}
	encode := func(res Results) (string, string) {
		report := BuildReport(res, ds, cfg)
		var jsonOut, mdOut bytes.Buffer
		if err := WriteJSON(&jsonOut, report); err != nil {
			t.Fatal(err)
		}
		if err := WriteMarkdown(&mdOut, report); err != nil {
			t.Fatal(err)
		}
		return jsonOut.String(), mdOut.String()
	}
	jsonA, mdA := encode(first)
	jsonB, mdB := encode(second)
	if jsonA != jsonB {
		t.Fatalf("JSON tidak identik:\n%s\n---\n%s", jsonA, jsonB)
	}
	if mdA != mdB {
		t.Fatalf("Markdown tidak identik:\n%s\n---\n%s", mdA, mdB)
	}
	if !strings.Contains(mdA, "| actual \\ predicted | A | B |") {
		t.Fatalf("header confusion hilang:\n%s", mdA)
	}
}

func TestMissingCostKeepsTotalNull(t *testing.T) {
	ds := fixtureDataset(t)
	arm := Arm{Name: "priced", Run: func(_ context.Context, input json.RawMessage) (ArmResult, error) {
		var in struct {
			I int `json:"i"`
		}
		_ = json.Unmarshal(input, &in)
		var cost *float64
		if in.I != 2 {
			cost = ptr(0.5)
		}
		out, _ := json.Marshal(map[string]any{"label": "A"})
		return ArmResult{Output: out, Stats: RunStats{Latency: time.Millisecond, Cost: cost}}, nil
	}}
	res, err := Execute(context.Background(), ds, []Arm{arm}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	usage := BuildReport(res, ds, MetricsConfig{}).Arms[0].Usage
	if usage.CostIncomplete != true || usage.CostUSD != nil || usage.ItemsMissingCost != 1 || usage.ItemsWithCost != 4 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestGroundingAndAgreementHooks(t *testing.T) {
	ds := fixtureDataset(t)
	res, err := Execute(context.Background(), ds, []Arm{fakeArm("base", []string{"A", "B", "B", "B", "A"})}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := MetricsConfig{
		LabelPath: "label",
		Ground: func(output json.RawMessage) (bool, bool) {
			return bytes.Contains(output, []byte(`"A"`)), true
		},
		Agree: func(output, label json.RawMessage) (bool, bool) {
			return bytes.Equal(output, label), true
		},
	}
	arm := BuildReport(res, ds, cfg).Arms[0]
	if !arm.Grounding.Measured || arm.Grounding.Items != 5 || arm.Grounding.Grounded != 2 || *arm.Grounding.Rate != 0.4 {
		t.Fatalf("grounding = %+v", arm.Grounding)
	}
	if !arm.Agreement.Measured || arm.Agreement.Items != 5 || arm.Agreement.Agreed != 4 || *arm.Agreement.Rate != 0.8 {
		t.Fatalf("agreement = %+v", arm.Agreement)
	}
	unmeasured := BuildReport(res, ds, MetricsConfig{LabelPath: "label"}).Arms[0]
	if unmeasured.Grounding.Measured || unmeasured.Agreement.Measured {
		t.Fatal("hooks yang tidak diberikan harus dilaporkan tidak terukur")
	}
}

func TestArmErrorIsRecordedNotFatal(t *testing.T) {
	ds := fixtureDataset(t)
	broken := Arm{Name: "broken", Run: func(context.Context, json.RawMessage) (ArmResult, error) {
		return ArmResult{Stats: RunStats{RunID: "custom"}}, errBoom
	}}
	res, err := Execute(context.Background(), ds, []Arm{broken}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	arm := BuildReport(res, ds, MetricsConfig{}).Arms[0]
	if arm.Errors != 5 {
		t.Fatalf("errors = %d", arm.Errors)
	}
	if row := res.Rows[0]; row.ErrorCode != "ARM_ERROR" || row.Err != "boom" {
		t.Fatalf("row = %+v", row)
	}
}

var errBoom = &boomError{}

type boomError struct{}

func (*boomError) Error() string { return "boom" }

func TestWrapAndRunIDHooks(t *testing.T) {
	ds := Dataset{Items: []Item{{ID: "x", Input: json.RawMessage(`{}`)}}}
	arm := Arm{Name: "inner", Run: func(context.Context, json.RawMessage) (ArmResult, error) {
		return ArmResult{Output: json.RawMessage(`{}`)}, nil
	}}
	res, err := Execute(context.Background(), ds, []Arm{arm}, Config{
		Wrap:  func(a Arm) Arm { return Arm{Name: "changed", Run: a.Run} },
		RunID: func(name, item string) string { return "run:" + name + ":" + item },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ArmNames[0] != "inner" || res.Rows[0].RunID != "run:inner:x" {
		t.Fatalf("names=%v row=%+v", res.ArmNames, res.Rows[0])
	}
}

func TestRulesArmEmitsHitsAndEvidence(t *testing.T) {
	subject := json.RawMessage(`{"type":"object","properties":{"amount":{"type":"number"}}}`)
	engine, err := rules.Load(subject, []rules.Rule{{
		ID: "big", Version: "1", Group: "g", Severity: "high",
		When: "amount > 10", Message: "nominal besar",
	}})
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	ds := Dataset{Items: []Item{{ID: "one", Input: json.RawMessage(`{"amount":20}`)}}}
	res, err := Execute(context.Background(), ds, []Arm{RulesArm("rules", engine, "g", func() time.Time { return fixed })}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	row := res.Rows[0]
	if !strings.Contains(string(row.Output), `"rule_id":"big"`) {
		t.Fatalf("output = %s", row.Output)
	}
	if len(row.Stats.Evidence) == 0 || row.Stats.Evidence[0].Kind != "RULE" {
		t.Fatalf("evidence = %+v", row.Stats.Evidence)
	}
	if got := sortKeys(map[string]int{"b": 1, "a": 2}); got[0] != "a" || got[1] != "b" {
		t.Fatalf("kunci tidak terurut: %v", got)
	}
}

func TestRatingCSVShape(t *testing.T) {
	ds := fixtureDataset(t)
	res, err := Execute(context.Background(), ds, []Arm{fakeArm("base", []string{"A", "B", "B", "B", "A"})}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := WriteRatingCSV(&out, res, ds); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 6 {
		t.Fatalf("baris CSV = %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "arm,item_id,run_id") || !strings.Contains(lines[1], "base,1,base#1") {
		t.Fatalf("CSV:\n%s", out.String())
	}
}
