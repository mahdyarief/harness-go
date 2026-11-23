package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hsg/internal/lib/ai"
)

type upperTransform struct{ sawOutbound string }

func (u *upperTransform) Outbound(messages []ai.Message) []ai.Message {
	out := make([]ai.Message, len(messages))
	for i, message := range messages {
		message.Content = strings.ToUpper(message.Content)
		out[i] = message
	}
	if len(out) > 0 {
		u.sawOutbound = out[0].Content
	}
	return out
}

func (u *upperTransform) Inbound(result ai.ChatResult) ai.ChatResult {
	result.Content = strings.ToLower(result.Content)
	return result
}

func TestWithTransformRewritesBothDirections(t *testing.T) {
	inner := &fakeLLM{result: ai.ChatResult{Content: "HASIL"}}
	var transform upperTransform
	result, err := WithTransform(inner, &transform).ChatStream(context.Background(), []ai.Message{{Role: "user", Content: "halo"}}, ai.ChatOptions{}, func(ai.StreamDelta) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if inner.lastMsg[0].Content != "HALO" || transform.sawOutbound != "HALO" {
		t.Fatalf("outbound content = %q", inner.lastMsg[0].Content)
	}
	if result.Content != "hasil" {
		t.Fatalf("inbound content = %q", result.Content)
	}
}

func TestWithTransformRestoresOnError(t *testing.T) {
	inner := &fakeLLM{result: ai.ChatResult{Content: "PARTIAL"}, err: errors.New("putus")}
	result, err := WithTransform(inner, &upperTransform{}).ChatStream(context.Background(), []ai.Message{{Role: "user", Content: "x"}}, ai.ChatOptions{}, func(ai.StreamDelta) error { return nil })
	if err == nil {
		t.Fatal("error must be forwarded")
	}
	if result.Content != "partial" {
		t.Fatalf("partial content must be restored: %q", result.Content)
	}
}
