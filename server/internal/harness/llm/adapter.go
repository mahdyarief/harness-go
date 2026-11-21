package llm

import (
	"context"
	"errors"

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
	result, err := a.gw.ChatStream(ctx, messages, opts, onDelta)
	if err == nil {
		return result, nil
	}
	return result, NewFailure("provider request failed", failureCode(err), err)
}

// failureCode maps a gateway error to a stable code. Only genuine transport
// faults become retryable codes: local rejections (input, config, response
// size) and client aborts are UNKNOWN so the retry policy leaves them alone.
// A stream that ended without [DONE] is STREAM_CLOSED, which DSH does not
// retry by default; the loop resumes it instead when text was streamed.
func failureCode(err error) string {
	if code := FailureCodeOf(err); code != "" {
		return code
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return string(CodeTimeout)
	case errors.Is(err, ai.ErrOutput):
		return string(CodeStreamClosed)
	case errors.Is(err, ai.ErrProvider):
		return string(CodeTransport)
	default:
		return string(CodeUnknown)
	}
}
