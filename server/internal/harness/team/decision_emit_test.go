package team

import (
	"encoding/json"
	"testing"
	"time"

	"hsg/internal/harness/services"
)

func TestEmitDecisionCarriesRuleAndValue(t *testing.T) {
	var got services.AIEvent
	sink := func(event services.AIEvent) error {
		got = event
		return nil
	}
	cfg := Config{
		ReduceName: func(Result) (string, json.RawMessage, bool) {
			return "all-structured", json.RawMessage(`{"value":"ok"}`), true
		},
	}
	result := Result{Members: []MemberResult{{Name: "a"}}, Duration: time.Millisecond}
	emitDecision(sink, cfg, result)

	if got.Type != "reduce/decision" {
		t.Fatalf("type = %q", got.Type)
	}
	var payload struct {
		Rule    string          `json:"rule"`
		Matched bool            `json:"matched"`
		Value   json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Rule != "all-structured" || !payload.Matched {
		t.Fatalf("payload = %+v", payload)
	}
	if string(payload.Value) != `{"value":"ok"}` {
		t.Fatalf("value = %s", payload.Value)
	}
}

func TestEmitDecisionSkipsWhenNoRuleMatched(t *testing.T) {
	called := false
	sink := func(services.AIEvent) error {
		called = true
		return nil
	}
	emitDecision(sink, Config{ReduceName: func(Result) (string, json.RawMessage, bool) {
		return "", nil, false
	}}, Result{})
	if called {
		t.Fatal("sink called without a winning rule")
	}
}
