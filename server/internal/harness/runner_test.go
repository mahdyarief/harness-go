package harness_test

import (
	"context"
	"errors"
	"testing"

	"hsg/internal/harness"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

type failingAgent struct{ out services.Outcome }

func (a failingAgent) Run(context.Context, services.RunRequest, services.Sink) (services.Outcome, error) {
	return a.out, errors.New("budget")
}

// The handler bills and persists from the outcome even when the run failed,
// so the runner must not replace it with a zero value on error.
func TestRunnerKeepsOutcomeOnError(t *testing.T) {
	s := scope.New(nil)
	agentOut := services.Outcome{
		Content: "sebagian", PromptTokens: 120, CompletionTokens: 30,
		Turns: 2, ToolCalls: 1, Partial: true,
		Tools: []services.ToolRecord{{Name: "lookup", Err: "TOOL_TIMEOUT"}},
	}
	if err := s.Provide(services.AgentKey, services.Agent(failingAgent{out: agentOut}), nil); err != nil {
		t.Fatal(err)
	}
	out, err := harness.NewRunner(s).Run(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("error must still be returned")
	}
	if out.PromptTokens != 120 || out.CompletionTokens != 30 || !out.Partial || out.Content != "sebagian" {
		t.Fatalf("outcome lost on error: %+v", out)
	}
	if out.Turns != 2 || out.ToolCalls != 1 || len(out.Tools) != 1 {
		t.Fatalf("counters lost on error: %+v", out)
	}
}
