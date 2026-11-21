package harness

import (
	"context"

	"github.com/google/uuid"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/core"
	"hsg/internal/harness/llm"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/tools"
	"hsg/internal/lib/agent"
	"hsg/internal/lib/ai"
)

type MountConfig struct {
	LoopConfig services.LoopConfig
}

func Mount(gw *ai.Gateway, registry *agent.Registry, cfg MountConfig) (*scope.Scope, error) {
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, llm.New(gw), nil); err != nil {
		return nil, err
	}
	if err := s.Provide(services.LoopConfigKey, cfg.LoopConfig, nil); err != nil {
		return nil, err
	}
	if err := s.Provide(services.ToolsKey, tools.Adapt(registry), nil); err != nil {
		return nil, err
	}
	if err := s.Provide(services.AgentLoopKey, services.AgentLoop(agentloop.New(s)), nil); err != nil {
		return nil, err
	}
	if err := s.Provide(services.AgentKey, core.NewAgent(s), nil); err != nil {
		return nil, err
	}
	return s, nil
}

// Runner adapts the mounted service graph to the legacy runner shape the chat
// handler uses, so SSE frames, persistence, and billing stay on proven types.
type Runner struct {
	scope *scope.Scope
}

func NewRunner(s *scope.Scope) *Runner { return &Runner{scope: s} }

func (r *Runner) Run(ctx context.Context, messages []ai.Message, sink agent.Sink) (agent.Outcome, error) {
	a, ok := scope.Require(r.scope, services.AgentKey)
	if !ok {
		return agent.Outcome{}, services.ErrLoopMissing
	}
	var adapted services.Sink
	if sink != nil {
		adapted = func(ev services.AIEvent) error {
			return sink(agent.Event{
				Type:       ev.Type,
				Text:       ev.Text,
				ToolName:   ev.ToolName,
				ToolCall:   ev.ToolCall,
				Error:      ev.Error,
				DurationMs: ev.DurationMs,
			})
		}
	}
	out, err := a.Run(ctx, services.RunRequest{Messages: messages}, adapted)
	if err != nil {
		return agent.Outcome{}, err
	}
	result := agent.Outcome{
		Content:          out.Content,
		PromptTokens:     out.PromptTokens,
		CompletionTokens: out.CompletionTokens,
		Turns:            out.Turns,
		ToolCalls:        out.ToolCalls,
		DurationMs:       out.DurationMs,
		Partial:          out.Partial,
	}
	for _, tr := range out.Tools {
		record := agent.ToolRecord{
			Turn:       tr.Turn,
			Name:       tr.Name,
			Arguments:  tr.Arguments,
			Result:     tr.Result,
			DurationMs: tr.DurationMs,
			Err:        tr.Err,
		}
		for _, ref := range tr.References {
			converted := adaptReference(ref)
			record.References = append(record.References, converted)
			result.References = appendUniqueReference(result.References, converted)
		}
		result.Tools = append(result.Tools, record)
	}
	for _, ref := range out.References {
		result.References = appendUniqueReference(result.References, adaptReference(ref))
	}
	return result, nil
}

func adaptReference(ref services.Reference) agent.Reference {
	docID, _ := uuid.Parse(ref.DocumentID)
	chunkID, _ := uuid.Parse(ref.ChunkID)
	return agent.Reference{
		DocumentID:  docID,
		ChunkID:     chunkID,
		Title:       ref.Title,
		Filename:    ref.Filename,
		Format:      ref.Format,
		Locator:     ref.Locator,
		HeadingPath: ref.HeadingPath,
		Position:    ref.Position,
		Excerpt:     ref.Excerpt,
		Source:      ref.Source,
	}
}

func appendUniqueReference(refs []agent.Reference, ref agent.Reference) []agent.Reference {
	for _, existing := range refs {
		if existing.ChunkID == ref.ChunkID {
			return refs
		}
	}
	return append(refs, ref)
}
