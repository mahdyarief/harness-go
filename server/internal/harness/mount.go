package harness

import (
	"context"

	"github.com/google/uuid"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/core"
	"hsg/internal/harness/llm"
	"hsg/internal/harness/preset"
	"hsg/internal/harness/sandbox"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/harness/tools"
	"hsg/internal/lib/agent"
	"hsg/internal/lib/ai"
)

type MountConfig struct {
	LoopConfig services.LoopConfig
	LoopGuards []services.LoopGuard
	Preset     string
	Sandbox    sandbox.Config
	// ApprovalAnswer resolves "ask" approvals; nil fails them closed.
	ApprovalAnswer services.ApprovalAnswer
}

func Mount(gw *ai.Gateway, registry *agent.Registry, cfg MountConfig) (*scope.Scope, error) {
	resolved, err := agentloop.ResolveLoopConfig(cfg.LoopConfig)
	if err != nil {
		return nil, err
	}
	resolved.LoopGuards = cfg.LoopGuards
	s := scope.New(nil)
	if err := s.Provide(services.LLMKey, llm.New(gw), nil); err != nil {
		return nil, err
	}
	if err := s.Provide(services.LoopConfigKey, resolved, nil); err != nil {
		return nil, err
	}
	presetName := cfg.Preset
	if presetName == "" {
		presetName = "read-only"
	}
	spec, ok := preset.Get(presetName)
	if !ok {
		return nil, preset.ErrUnknown
	}
	sandboxCfg := cfg.Sandbox
	if sandboxCfg.Mode == "" {
		sandboxCfg.Mode = spec.SandboxMode
	}
	if err := sandbox.Validate(sandboxCfg); err != nil {
		return nil, err
	}
	if err := s.Provide(services.SandboxKey, sandboxCfg.EffectiveMode(), nil); err != nil {
		return nil, err
	}
	if err := s.Provide(services.ApprovalKey, spec.Approval, nil); err != nil {
		return nil, err
	}
	if err := s.Provide(services.ToolAllowlistKey, spec.ToolAllowlist, nil); err != nil {
		return nil, err
	}
	if cfg.ApprovalAnswer != nil {
		if err := s.Provide(services.ApprovalAnswerKey, cfg.ApprovalAnswer, nil); err != nil {
			return nil, err
		}
	}
	adapted := tools.Adapt(registry)
	if err := tools.ValidateSchemas(adapted); err != nil {
		return nil, err
	}
	if err := s.Provide(services.ToolsKey, tools.WrapTools(s, adapted, resolved), nil); err != nil {
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
	// The outcome is converted even when the run failed: the handler bills the
	// tokens already spent and persists the partial answer from it.
	out, err := a.Run(ctx, services.RunRequest{Messages: messages}, adapted)
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
	return result, err
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
