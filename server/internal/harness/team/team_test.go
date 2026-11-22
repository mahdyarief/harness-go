package team

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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

func (memberTool) Name() string        { return "submit" }
func (memberTool) Description() string { return "submit result" }
func (memberTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)
}
func (memberTool) ReadOnly() bool { return true }
func (memberTool) Execute(context.Context, json.RawMessage) (agent.ToolResult, error) {
	return agent.ToolResult{}, nil
}
func (memberTool) Terminal() bool { return true }

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
		if messages[0].Content == "slow" {
			<-ctx.Done()
			return ai.ChatResult{}, ctx.Err()
		}
		return submitLLM(ctx, nil, ai.ChatOptions{}, nil)
	}}
	parent := teamParent(t, llm)
	slow := profileFor("slow")
	slow.System = "slow"
	members := []Member{{Profile: slow}, {Profile: profileFor("fast-a")}, {Profile: profileFor("fast-b")}}
	started := time.Now()
	got, err := Run(context.Background(), parent, members, Config{MaxParallel: 3, MemberTimeout: 50 * time.Millisecond}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 45*time.Millisecond || elapsed > 500*time.Millisecond {
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
	if gotEvent.Agent != "named" || gotEvent.Type != "tool_start" {
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

func profileFor(name string) profile.Profile {
	return profile.Profile{Name: name, System: "test", Tools: []string{}, Result: &services.ResultSpec{Name: "submit", Schema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)}}
}
