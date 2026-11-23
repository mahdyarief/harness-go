package team

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hsg/internal/harness/cost"
	"hsg/internal/harness/profile"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/agent"
	"hsg/internal/lib/ai"
)

type memberLLM struct {
	fn       func(context.Context, []ai.Message, ai.ChatOptions, func(ai.StreamDelta) error) (ai.ChatResult, error)
	messages chan []ai.Message
}

func (*memberLLM) MaxTokens() int { return 1024 }
func (m *memberLLM) ChatStream(ctx context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	if m.messages != nil {
		m.messages <- messages
	}
	return m.fn(ctx, messages, opts, onDelta)
}

type memberTool struct{}

// The registry tool must not share a name with the profile result tool: a
// profile owns its submit tool, so a collision is a mount error.
func (memberTool) Name() string        { return "lookup" }
func (memberTool) Description() string { return "lookup" }
func (memberTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)
}
func (memberTool) ReadOnly() bool { return true }
func (memberTool) Execute(context.Context, json.RawMessage) (agent.ToolResult, error) {
	return agent.ToolResult{}, nil
}
func (memberTool) Terminal() bool { return false }

func teamParent(t *testing.T, llm services.LLM) *scope.Scope {
	t.Helper()
	s := scope.New(nil)
	if err := s.Provide(services.AgentRegistryKey, agent.NewRegistry(memberTool{}), nil); err != nil {
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

func submitLLM(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	return ai.ChatResult{ToolCalls: []ai.ToolCall{{ID: "submit", Type: "function", Function: ai.ToolCallFunction{Name: "submit", Arguments: `{"value":"ok"}`}}}}, nil
}

func TestRunTimesOutOneMemberWithoutCancelingOthers(t *testing.T) {
	llm := &memberLLM{fn: func(ctx context.Context, messages []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
		for _, message := range messages {
			if message.Content == "slow" {
				<-ctx.Done()
				return ai.ChatResult{}, ctx.Err()
			}
		}
		return submitLLM(ctx, nil, ai.ChatOptions{}, nil)
	}}
	parent := teamParent(t, llm)
	slow := profileFor("slow")
	slow.System = "slow"
	members := []Member{{Profile: slow}, {Profile: profileFor("fast-a")}, {Profile: profileFor("fast-b")}}
	started := time.Now()
	// The timeout is shared by every member, so it must be generous enough
	// that the fake fast paths finish even on a slow instrumented Windows run.
	got, err := Run(context.Background(), parent, members, Config{MaxParallel: 3, MemberTimeout: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("elapsed %v", elapsed)
	}
	if len(got.Members) != 3 || got.Members[0].ErrCode != "AGENT_TIMEOUT" || got.Members[1].Err != nil || got.Members[2].Err != nil {
		t.Fatalf("unexpected results: %+v", got.Members)
	}
	if got.Members[1].Outcome.Structured == nil || got.Members[2].Outcome.Structured == nil {
		t.Fatal("successful member results missing")
	}
}

func TestRunAppliesProfilePromptAndLabelsEvents(t *testing.T) {
	messages := make(chan []ai.Message, 1)
	parent := teamParent(t, &memberLLM{fn: submitLLM, messages: messages})
	var gotEvent services.AIEvent
	_, err := Run(context.Background(), parent, []Member{{Profile: profileFor("named")}}, Config{MaxParallel: 1, MemberTimeout: time.Second}, func(e services.AIEvent) error {
		gotEvent = e
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case history := <-messages:
		if len(history) == 0 || history[0].Role != "system" || history[0].Content != "test" {
			t.Fatalf("profile system prompt not applied: %+v", history)
		}
	default:
		t.Fatal("model not called")
	}
	// The last event of a successful submission is the structured/submit
	// row, labelled with the member's name.
	if gotEvent.Agent != "named" || gotEvent.Type != "structured/submit" || gotEvent.Payload == nil {
		t.Fatalf("event = %+v", gotEvent)
	}
}

func TestRunPreservesInputOrderAcrossRepeatedRuns(t *testing.T) {
	parent := teamParent(t, &memberLLM{fn: submitLLM})
	members := []Member{{Profile: profileFor("a")}, {Profile: profileFor("b")}, {Profile: profileFor("c")}}
	for range 50 {
		got, err := Run(context.Background(), parent, members, Config{MaxParallel: 3, MemberTimeout: time.Second}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range []string{"a", "b", "c"} {
			if got.Members[i].Name != want {
				t.Fatalf("order: got %q at %d, want %q", got.Members[i].Name, i, want)
			}
		}
	}
}

func TestRunForwardsAgentAndParentCancellation(t *testing.T) {
	started := make(chan struct{}, 3)
	parent := teamParent(t, &memberLLM{fn: func(ctx context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		return ai.ChatResult{}, ctx.Err()
	}})
	members := []Member{{Profile: profileFor("a")}, {Profile: profileFor("b")}, {Profile: profileFor("c")}}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan Result, 1)
	go func() {
		r, _ := Run(ctx, parent, members, Config{MaxParallel: 3, MemberTimeout: time.Second}, func(e services.AIEvent) error { return nil })
		result <- r
	}()
	for range 3 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("members did not start")
		}
	}
	cancel()
	select {
	case got := <-result:
		if len(got.Members) != 3 {
			t.Fatalf("got %d results", len(got.Members))
		}
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not stop members")
	}
}

// usageLLM bills a fixed token count on every call and then submits, so a
// member's Outcome carries exactly those tokens.
func usageLLM(prompt, completion int) *memberLLM {
	return &memberLLM{fn: func(context.Context, []ai.Message, ai.ChatOptions, func(ai.StreamDelta) error) (ai.ChatResult, error) {
		return ai.ChatResult{
			PromptTokens: prompt, CompletionTokens: completion,
			ToolCalls: []ai.ToolCall{{ID: "submit", Type: "function", Function: ai.ToolCallFunction{Name: "submit", Arguments: `{"value":"ok"}`}}},
		}, nil
	}}
}

func priceTable(t *testing.T) *cost.Table {
	t.Helper()
	table, err := cost.NewTable("2026-02-01", map[string]cost.Rates{
		"murah": {InputPerMillion: 2, OutputPerMillion: 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &table
}

func closeEnough(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-9
}

func TestRunAccountsCostPerMemberAndTotal(t *testing.T) {
	parent := teamParent(t, usageLLM(1000, 500))
	members := []Member{
		{Model: "murah", Profile: profileFor("a")},
		{Model: "murah", Profile: profileFor("b")},
		{Model: "murah", Profile: profileFor("c")},
	}
	got, err := Run(context.Background(), parent, members, Config{MaxParallel: 2, MemberTimeout: time.Second, Cost: priceTable(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %v", got.Warnings)
	}
	// 1000 prompt at $2/1M plus 500 completion at $8/1M.
	wantPerMember := 1000.0/1e6*2 + 500.0/1e6*8
	var sum float64
	for i, member := range got.Members {
		if member.CostUSD == nil {
			t.Fatalf("member %d cost is nil", i)
		}
		if !closeEnough(*member.CostUSD, wantPerMember) {
			t.Fatalf("member %d cost = %v; want %v", i, *member.CostUSD, wantPerMember)
		}
		sum += *member.CostUSD
	}
	if got.CostUSD == nil || !closeEnough(*got.CostUSD, sum) {
		t.Fatalf("total cost = %v; want %v", got.CostUSD, sum)
	}
}

func TestRunUnpricedModelWarnsWithoutFailing(t *testing.T) {
	parent := teamParent(t, usageLLM(1000, 500))
	members := []Member{
		{Model: "hantu", Profile: profileFor("a")},
		{Model: "murah", Profile: profileFor("b")},
	}
	got, err := Run(context.Background(), parent, members, Config{MaxParallel: 2, MemberTimeout: time.Second, Cost: priceTable(t)}, nil)
	if err != nil {
		t.Fatalf("an unpriced model must not fail the run: %v", err)
	}
	if got.Members[0].Err != nil {
		t.Fatalf("unpriced member failed: %v", got.Members[0].Err)
	}
	if got.Members[0].CostUSD != nil {
		t.Fatalf("unpriced member cost = %v; want nil", *got.Members[0].CostUSD)
	}
	if got.Members[1].CostUSD == nil {
		t.Fatal("priced member cost is nil")
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "hantu") {
		t.Fatalf("warnings = %v", got.Warnings)
	}
	if got.CostUSD == nil || !closeEnough(*got.CostUSD, *got.Members[1].CostUSD) {
		t.Fatalf("total = %v; want only the priced member", got.CostUSD)
	}
}

func TestRunWithoutPriceTableLeavesCostNil(t *testing.T) {
	parent := teamParent(t, usageLLM(1000, 500))
	got, err := Run(context.Background(), parent, []Member{{Model: "murah", Profile: profileFor("a")}}, Config{MaxParallel: 1, MemberTimeout: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.CostUSD != nil || got.Members[0].CostUSD != nil {
		t.Fatal("cost was computed without a price table")
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %v", got.Warnings)
	}
}

func TestMemberDurationIsRecordedPerMember(t *testing.T) {
	llm := &memberLLM{fn: func(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
		time.Sleep(5 * time.Millisecond)
		return submitLLM(context.Background(), nil, ai.ChatOptions{}, nil)
	}}
	result, err := Run(context.Background(), teamParent(t, llm),
		[]Member{{Profile: profileFor("a")}, {Profile: profileFor("b")}},
		Config{MaxParallel: 1, MemberTimeout: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range result.Members {
		if member.Duration < 5*time.Millisecond {
			t.Fatalf("%s duration = %s; want at least the simulated work", member.Name, member.Duration)
		}
		if member.Duration > result.Duration {
			t.Fatalf("%s duration = %s exceeds the team duration %s", member.Name, member.Duration, result.Duration)
		}
	}
}

func profileFor(name string) profile.Profile {
	return profile.Profile{Name: name, System: "test", Tools: []string{}, Result: &services.ResultSpec{Name: "submit", Schema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)}}
}
