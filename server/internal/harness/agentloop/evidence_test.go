package agentloop_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/evidence"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

// repairLLM reads once, submits an invented citation, then repairs it to the
// chunk the tool actually returned.
type repairLLM struct{ calls int }

func (l *repairLLM) MaxTokens() int { return 1024 }
func (l *repairLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	if l.calls == 1 {
		return ai.ChatResult{
			ToolCalls:        []ai.ToolCall{{ID: "r", Type: "function", Function: ai.ToolCallFunction{Name: "read_doc", Arguments: `{}`}}},
			FinishReason:     "tool_calls",
			PromptTokens:     3,
			CompletionTokens: 1,
		}, nil
	}
	ref := "chunk-fake"
	if l.calls > 2 {
		ref = "chunk-real"
	}
	args, _ := json.Marshal(map[string]any{
		"answer":   "selesai",
		"evidence": []map[string]string{{"kind": "DOCUMENT", "ref": ref}},
	})
	return ai.ChatResult{
		ToolCalls:        []ai.ToolCall{{ID: "t", Type: "function", Function: ai.ToolCallFunction{Name: "submit", Arguments: string(args)}}},
		FinishReason:     "tool_calls",
		PromptTokens:     5,
		CompletionTokens: 1,
	}, nil
}

// stuckLLM reads once and then keeps citing a ref no tool returned, so no
// repair can succeed. The fake ref varies per call so the repeat-call guard
// does not deny the (intentional) resubmission.
type stuckLLM struct{ calls int }

func (l *stuckLLM) MaxTokens() int { return 1024 }
func (l *stuckLLM) ChatStream(_ context.Context, _ []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	l.calls++
	if l.calls == 1 {
		return ai.ChatResult{
			ToolCalls:        []ai.ToolCall{{ID: "r", Type: "function", Function: ai.ToolCallFunction{Name: "read_doc", Arguments: `{}`}}},
			FinishReason:     "tool_calls",
			PromptTokens:     3,
			CompletionTokens: 1,
		}, nil
	}
	args, _ := json.Marshal(map[string]any{
		"answer":   "selesai",
		"evidence": []map[string]string{{"kind": "DOCUMENT", "ref": fmt.Sprintf("chunk-fake-%d", l.calls)}},
	})
	return ai.ChatResult{
		ToolCalls:        []ai.ToolCall{{ID: "t", Type: "function", Function: ai.ToolCallFunction{Name: "submit", Arguments: string(args)}}},
		FinishReason:     "tool_calls",
		PromptTokens:     5,
		CompletionTokens: 1,
	}, nil
}

type citingTool struct{}

func (citingTool) Name() string        { return "read_doc" }
func (citingTool) Description() string { return "read a document" }
func (citingTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (citingTool) ReadOnly() bool { return true }
func (citingTool) Execute(_ context.Context, _ json.RawMessage) (services.ToolResult, error) {
	return services.ToolResult{
		Output: "isi dokumen",
		Evidence: []evidence.Item{{
			Kind: "DOCUMENT", Ref: "chunk-real", Excerpt: "isi dokumen",
			Meta: map[string]string{"title": "Dokumen"},
		}},
	}, nil
}

type submittingTool struct{}

func (submittingTool) Name() string        { return "submit" }
func (submittingTool) Description() string { return "submit the result" }
func (submittingTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"},"evidence":{"type":"array","items":{"type":"object","properties":{"kind":{"type":"string"},"ref":{"type":"string"}},"required":["kind","ref"]}}},"required":["answer","evidence"]}`)
}
func (submittingTool) ReadOnly() bool { return false }
func (submittingTool) Execute(_ context.Context, _ json.RawMessage) (services.ToolResult, error) {
	return services.ToolResult{Output: "submitted"}, nil
}
func (submittingTool) Terminal() bool { return true }

type staticToolSet struct{ tools []services.Tool }

func (s staticToolSet) Lookup(name string) (services.Tool, bool) {
	for _, tool := range s.tools {
		if tool.Name() == name {
			return tool, true
		}
	}
	return nil, false
}
func (s staticToolSet) Specs() []ai.ToolSpec {
	specs := make([]ai.ToolSpec, 0, len(s.tools))
	for _, tool := range s.tools {
		specs = append(specs, ai.ToolSpec{Type: "function", Function: ai.ToolFunction{
			Name: tool.Name(), Description: tool.Description(), Parameters: tool.Parameters(),
		}})
	}
	return specs
}

func TestUngroundedCitationIsRepaired(t *testing.T) {
	llm := &repairLLM{}
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, services.LLM(llm), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.ToolsKey, services.Tools(staticToolSet{tools: []services.Tool{citingTool{}, submittingTool{}}}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, services.LoopConfig{
		MaxTurns:             5,
		RequireStructured:    "submit",
		MaxStructuredRepairs: 2,
		EvidenceRegistry:     evidence.NewRegistry("DOCUMENT"),
		EvidencePath:         "evidence",
	}, nil); err != nil {
		t.Fatal(err)
	}

	var notices []string
	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{
		Messages: []ai.Message{{Role: "user", Content: "baca lalu submit"}},
	}, func(ev services.AIEvent) error {
		if ev.Type == "notice" {
			notices = append(notices, ev.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("run err = %v", err)
	}
	if llm.calls != 3 {
		t.Fatalf("llm calls = %d; want 3 (read, invented, repaired)", llm.calls)
	}
	if out.Partial {
		t.Fatal("repaired run must not be partial")
	}
	var body struct {
		Answer   string `json:"answer"`
		Evidence []struct {
			Kind string `json:"kind"`
			Ref  string `json:"ref"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(out.Structured, &body); err != nil {
		t.Fatal(err)
	}
	if body.Answer != "selesai" || len(body.Evidence) != 1 || body.Evidence[0].Ref != "chunk-real" {
		t.Fatalf("structured = %s", out.Structured)
	}
	found := false
	for _, notice := range notices {
		if strings.Contains(notice, "evidence ungrounded") {
			found = true
		}
	}
	if !found {
		t.Fatalf("repair notice missing: %v", notices)
	}
}

func TestUngroundedSubmissionExhaustsRepairs(t *testing.T) {
	llm := &stuckLLM{}
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, services.LLM(llm), nil); err != nil {
		t.Fatal(err)
	}
	// The citing tool runs, but the model keeps inventing a ref the tool
	// never returned, so every submission stays ungrounded.
	if err := s.Provide(services.ToolsKey, services.Tools(staticToolSet{tools: []services.Tool{citingTool{}, submittingTool{}}}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, services.LoopConfig{
		MaxTurns:             5,
		RequireStructured:    "submit",
		MaxStructuredRepairs: 1,
		EvidenceRegistry:     evidence.NewRegistry("DOCUMENT"),
		EvidencePath:         "evidence",
	}, nil); err != nil {
		t.Fatal(err)
	}

	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{
		Messages: []ai.Message{{Role: "user", Content: "submit"}},
	}, nil)
	if err == nil {
		t.Fatal("expected EVIDENCE_UNGROUNDED after exhausting repairs")
	}
	if services.ErrorCode(err) != "EVIDENCE_UNGROUNDED" {
		t.Fatalf("err = %v (%s); want EVIDENCE_UNGROUNDED", err, services.ErrorCode(err))
	}
	if !out.Partial {
		t.Fatal("exhausted repairs must leave the run partial")
	}
}

func TestNoRegistryKeepsLegacyBehaviour(t *testing.T) {
	llm := &repairLLM{}
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, services.LLM(llm), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.ToolsKey, services.Tools(staticToolSet{tools: []services.Tool{citingTool{}, submittingTool{}}}), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Provide(services.LoopConfigKey, services.LoopConfig{
		MaxTurns: 5, RequireStructured: "submit", MaxStructuredRepairs: 1,
	}, nil); err != nil {
		t.Fatal(err)
	}

	out, err := agentloop.New(s).Run(context.Background(), services.RunRequest{
		Messages: []ai.Message{{Role: "user", Content: "submit"}},
	}, nil)
	if err != nil {
		t.Fatalf("run err = %v", err)
	}
	// Without a registry the invented citation is accepted as-is.
	if llm.calls != 2 {
		t.Fatalf("llm calls = %d; want 2 (read, submit)", llm.calls)
	}
	if out.Structured == nil {
		t.Fatal("structured submission missing")
	}
}
