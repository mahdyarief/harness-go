package runlog_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hsg/internal/harness/profile"
	"hsg/internal/harness/runlog"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/team"
	"hsg/internal/lib/agent"
	"hsg/internal/lib/ai"
)

type loggedLLM struct{ calls int }

func (*loggedLLM) MaxTokens() int { return 1024 }
func (l *loggedLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	return ai.ChatResult{
		ToolCalls:        []ai.ToolCall{{ID: "t", Type: "function", Function: ai.ToolCallFunction{Name: "submit", Arguments: `{"value":"ok"}`}}},
		PromptTokens:     7,
		CompletionTokens: 2,
		FinishReason:     "tool_calls",
	}, nil
}

type loggedTool struct{}

func (loggedTool) Name() string        { return "lookup" }
func (loggedTool) Description() string { return "lookup" }
func (loggedTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)
}
func (loggedTool) ReadOnly() bool { return true }
func (loggedTool) Execute(_ context.Context, _ json.RawMessage) (agent.ToolResult, error) {
	return agent.ToolResult{Output: "submitted"}, nil
}
func (loggedTool) Terminal() bool { return true }

func loggedParent(t *testing.T, llm services.LLM) *scope.Scope {
	t.Helper()
	s := scope.New(nil)
	if err := s.Provide(services.AgentRegistryKey, agent.NewRegistry(loggedTool{}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LLMKey, llm, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, services.LoopConfig{MaxTurns: 3}, nil); err != nil {
		t.Fatal(err)
	}
	return s
}

func loggedProfile(name string) profile.Profile {
	return profile.Profile{
		Name:   name,
		System: "sys",
		// submit is synthesized by the profile itself, so it is not a registry
		// entry and must not be listed here.
		Tools:  nil,
		Result: &services.ResultSpec{Name: "submit", Schema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)},
	}
}

func TestSinkRecordsContiguousSeqAndReplays(t *testing.T) {
	llm := &loggedLLM{}
	parent := loggedParent(t, llm)
	store := runlog.NewMemory()
	runID := "00000000-0000-0000-0000-000000000001"
	member := "worker"

	if err := store.CreateRun(context.Background(), services.RunRecord{ID: runID}); err != nil {
		t.Fatal(err)
	}
	if err := runlog.RunStart(context.Background(), store, runID, member); err != nil {
		t.Fatal(err)
	}
	sink := runlog.Sink(context.Background(), store, runID, member)
	result, err := team.Run(context.Background(), parent, []team.Member{{Profile: loggedProfile(member)}},
		team.Config{MaxParallel: 1, MemberTimeout: time.Second}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if err := runlog.RunEnd(context.Background(), store, runID, member, "ok"); err != nil {
		t.Fatal(err)
	}

	events, err := store.Events(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("no events recorded")
	}
	for i, event := range events {
		if event.Seq != int64(i+1) {
			t.Fatalf("seq %d at index %d (want contiguous from 1)", event.Seq, i)
		}
		if event.RunID != runID {
			t.Fatalf("run id %q", event.RunID)
		}
	}
	types := make([]string, len(events))
	for i, event := range events {
		types[i] = event.Type
	}
	if types[0] != runlog.EventTypeRunStart || types[len(types)-1] != runlog.EventTypeRunEnd {
		t.Fatalf("missing lifecycle events: %v", types)
	}
	// The column is NOT NULL and jsonb-typed: an empty or null payload would
	// be rejected by Postgres, so every lifecycle row is a JSON object.
	if got := string(events[0].Payload); got != "{}" {
		t.Fatalf("run/start payload = %q, want {}", got)
	}
	foundModel, foundToolResult, foundStructured := false, false, false
	for _, typ := range types {
		switch typ {
		case runlog.EventTypeModelCall:
			foundModel = true
		case runlog.EventTypeToolResult:
			foundToolResult = true
		case runlog.EventTypeStructured:
			foundStructured = true
		}
	}
	if !foundModel || !foundToolResult || !foundStructured {
		t.Fatalf("missing event types: %v", types)
	}

	projection := runlog.Project(events)
	if !projection.Started || !projection.Ended || projection.Status != "ok" {
		t.Fatalf("lifecycle: %+v", projection)
	}
	if projection.PromptTokens != result.PromptTokens || projection.CompletionTokens != result.CompletionTokens {
		t.Fatalf("tokens: projected %d/%d, result %d/%d",
			projection.PromptTokens, projection.CompletionTokens, result.PromptTokens, result.CompletionTokens)
	}
	if len(projection.Members) != 1 || projection.Members[0].Name != member {
		t.Fatalf("members: %+v", projection.Members)
	}
	if len(projection.Members[0].Structured) == 0 {
		t.Fatalf("structured output not projected: %+v", projection.Members[0])
	}
}

func TestSinkStripsPromptTextByDefault(t *testing.T) {
	store := runlog.NewMemory()
	runID := "00000000-0000-0000-0000-000000000002"
	if err := store.CreateRun(context.Background(), services.RunRecord{ID: runID}); err != nil {
		t.Fatal(err)
	}
	sink := runlog.Sink(context.Background(), store, runID, "m")
	call := runlog.ModelCall{
		PromptTokens: 1, CompletionTokens: 1, RequestHash: runlog.HashRequest("secret prompt"),
		Prompt: "secret prompt",
	}
	payload, _ := json.Marshal(call)
	if err := sink(services.AIEvent{Type: runlog.EventTypeModelCall, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	events, err := store.Events(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	var logged runlog.ModelCall
	if err := json.Unmarshal(events[0].Payload, &logged); err != nil {
		t.Fatal(err)
	}
	if logged.Prompt != "" {
		t.Fatalf("prompt text leaked: %q", logged.Prompt)
	}
	if logged.RequestHash != call.RequestHash {
		t.Fatalf("request hash lost: %q", logged.RequestHash)
	}

	// With LogPrompts on the prompt text is kept.
	sink = runlog.Sink(context.Background(), store, runID, "m", runlog.SinkOpts{Prompts: runlog.LogPrompts{Enabled: true}})
	if err := sink(services.AIEvent{Type: runlog.EventTypeModelCall, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	events, _ = store.Events(context.Background(), runID)
	if err := json.Unmarshal(events[1].Payload, &logged); err != nil {
		t.Fatal(err)
	}
	if logged.Prompt != "secret prompt" {
		t.Fatalf("opt-in prompt missing: %q", logged.Prompt)
	}
}

func TestSinkKeepsToolCallArguments(t *testing.T) {
	store := runlog.NewMemory()
	runID := "00000000-0000-0000-0000-00000000000c"
	if err := store.CreateRun(context.Background(), services.RunRecord{ID: runID}); err != nil {
		t.Fatal(err)
	}
	sink := runlog.Sink(context.Background(), store, runID, "")
	args := `{"q":"kucing"}`
	payload, err := json.Marshal(runlog.ToolCall{Name: "lookup", Args: args, CallID: "call-7"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink(services.AIEvent{Type: "tool_start", ToolName: "lookup", ToolCall: "call-7", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	events, err := store.Events(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != runlog.EventTypeToolCall {
		t.Fatalf("events = %+v", events)
	}
	var got runlog.ToolCall
	if err := json.Unmarshal(events[0].Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "lookup" || got.Args != args || got.CallID != "call-7" {
		t.Fatalf("payload = %+v; arguments lost", got)
	}
}

func TestProjectionSumsPricedModelCalls(t *testing.T) {
	price := 0.25
	events := []services.RunLogEvent{
		runlog.NewModelCallEvent("run-cost", "a", runlog.ModelCall{Model: "m", PromptTokens: 10, CostUSD: &price}, runlog.LogPrompts{}),
		runlog.NewModelCallEvent("run-cost", "b", runlog.ModelCall{Model: "x", PromptTokens: 3}, runlog.LogPrompts{}),
	}
	projection := runlog.Project(events)
	if !projection.CostKnown || projection.CostUSD != price {
		t.Fatalf("projection cost = %v (known %v); want %v", projection.CostUSD, projection.CostKnown, price)
	}
	for _, member := range projection.Members {
		switch member.Name {
		case "a":
			if !member.CostKnown || member.CostUSD != price {
				t.Fatalf("member a cost = %v (known %v)", member.CostUSD, member.CostKnown)
			}
		case "b":
			if member.CostKnown {
				t.Fatalf("unpriced member b reported a cost")
			}
		}
	}
}
