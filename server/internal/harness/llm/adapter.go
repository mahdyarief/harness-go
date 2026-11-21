package llm

import (
	"context"

	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

type adapter struct{ gw *ai.Gateway }

func New(gw *ai.Gateway) services.LLM { return &adapter{gw: gw} }

func (a *adapter) MaxTokens() int {
	if a == nil || a.gw == nil {
		return 0
	}
	return a.gw.MaxTokens()
}

func (a *adapter) ChatStream(ctx context.Context, messages []ai.Message, opts ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	if a == nil || a.gw == nil {
		return ai.ChatResult{}, ai.ErrConfig
	}
	return a.gw.ChatStream(ctx, messages, opts, onDelta)
}
