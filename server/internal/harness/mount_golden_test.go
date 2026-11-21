package harness_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hsg/internal/harness"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/agent"
	"hsg/internal/lib/ai"
)

type goldenAgent struct{}

func (goldenAgent) Run(_ context.Context, _ services.RunRequest, sink services.Sink) (services.Outcome, error) {
	if sink != nil {
		_ = sink(services.AIEvent{Type: "delta", Text: "halo"})
	}
	return services.Outcome{
		Content:          "halo",
		PromptTokens:     11,
		CompletionTokens: 3,
		Turns:            1,
		ToolCalls:        1,
		Tools: []services.ToolRecord{{
			Turn: 0, Name: "lookup", Arguments: `{"q":"x"}`, Result: "hasil:x", DurationMs: 2,
			References: []services.Reference{{
				DocumentID: "11111111-1111-1111-1111-111111111111",
				ChunkID:    "22222222-2222-2222-2222-222222222222",
				Title:      "Dok",
			}},
		}},
		DurationMs: 7,
	}, nil
}

func TestHarnessRewriteKeepsSSEAndBilling(t *testing.T) {
	s := scope.New(nil)
	if err := s.Provide(services.AgentKey, goldenAgent{}, nil); err != nil {
		t.Fatal(err)
	}
	runner := harness.NewRunner(s)

	var frames []string
	out, err := runner.Run(context.Background(), []ai.Message{{Role: "user", Content: "hai"}}, func(ev agent.Event) error {
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		frames = append(frames, "data: "+string(b)+"\n\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || !strings.Contains(frames[0], `"type":"delta"`) || !strings.Contains(frames[0], `"text":"halo"`) {
		t.Fatalf("frames = %v; want one delta frame with text halo", frames)
	}
	if out.Content != "halo" || out.PromptTokens != 11 || out.CompletionTokens != 3 || out.DurationMs != 7 {
		t.Fatalf("outcome mismatch: %+v", out)
	}
	if len(out.Tools) != 1 || out.Tools[0].Name != "lookup" || out.Tools[0].Result != "hasil:x" {
		t.Fatalf("tool records = %+v", out.Tools)
	}
	if len(out.References) != 1 || out.References[0].Title != "Dok" {
		t.Fatalf("references = %+v", out.References)
	}
}
