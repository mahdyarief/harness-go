package llm

import (
	"context"

	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

// Transform rewrites the conversation on its way to the provider and the
// result on its way back. Outbound runs first, so a redactor can replace
// identifiers the provider must never see; Inbound restores them before the
// loop and the tools use the text.
type Transform interface {
	Outbound(messages []ai.Message) []ai.Message
	Inbound(result ai.ChatResult) ai.ChatResult
}

type transformer struct {
	inner services.LLM
	t     Transform
}

// WithTransform wraps inner with t. Deltas themselves are not transformed:
// they are shown to the user as they arrive, and a restored token spanning
// two chunks cannot be recovered reliably. The final result is restored.
func WithTransform(inner services.LLM, t Transform) services.LLM {
	return &transformer{inner: inner, t: t}
}

func (x *transformer) MaxTokens() int { return x.inner.MaxTokens() }

func (x *transformer) ChatStream(ctx context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	result, err := x.inner.ChatStream(ctx, x.t.Outbound(messages), opts, onDelta)
	// Restore even on error: a partial answer may still carry pseudonyms the
	// caller will show or persist.
	return x.t.Inbound(result), err
}
