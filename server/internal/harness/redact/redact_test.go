package redact

import (
	"context"
	"strings"
	"testing"

	"hsg/internal/harness/llm"
	"hsg/internal/lib/ai"
)

type captureLLM struct {
	captured []ai.Message
	reply    ai.ChatResult
}

func (c *captureLLM) MaxTokens() int { return 10 }

func (c *captureLLM) ChatStream(_ context.Context, messages []ai.Message, _ ai.ChatOptions, onDelta func(ai.StreamDelta) error) (ai.ChatResult, error) {
	c.captured = messages
	if err := onDelta(ai.StreamDelta{Content: c.reply.Content}); err != nil {
		return ai.ChatResult{}, err
	}
	return c.reply, nil
}

func emailAndPhone(t *testing.T) []Pattern {
	t.Helper()
	email, err := NewPattern("EMAIL", `[\w.+-]+@[\w-]+\.[\w.]+`)
	if err != nil {
		t.Fatal(err)
	}
	phone, err := NewPattern("PHONE", `\+?\d{2,4}[- ]?\d{3,4}[- ]?\d{3,4}`)
	if err != nil {
		t.Fatal(err)
	}
	return []Pattern{email, phone}
}

func TestOutboundHidesOriginalsAndInboundRestores(t *testing.T) {
	patterns := emailAndPhone(t)
	redactor := New(patterns...)
	inner := &captureLLM{reply: ai.ChatResult{
		Content: "kirim ke alamat yang sama",
		ToolCalls: []ai.ToolCall{{
			ID: "call-1", Type: "function",
			Function: ai.ToolCallFunction{Name: "send", Arguments: `{"to":"a@b.com"}`},
		}},
	}}
	messages := []ai.Message{
		{Role: "system", Content: "hubungi a@b.com atau 0812-3456-7890"},
		{Role: "user", Content: "ulangi a@b.com"},
	}

	outbound := redactor.Outbound(messages)
	for _, message := range outbound {
		if strings.Contains(message.Content, "a@b.com") || strings.Contains(message.Content, "0812") {
			t.Fatalf("original leaked outbound: %q", message.Content)
		}
	}
	if !strings.Contains(outbound[0].Content, "<EMAIL_1>") || !strings.Contains(outbound[0].Content, "<PHONE_1>") {
		t.Fatalf("pseudonyms missing: %q", outbound[0].Content)
	}
	if !strings.Contains(outbound[1].Content, "<EMAIL_1>") {
		t.Fatalf("the same value must map to the same pseudonym: %q", outbound[1].Content)
	}
	if messages[0].Content != "hubungi a@b.com atau 0812-3456-7890" {
		t.Fatal("Outbound must not mutate the caller's messages")
	}

	// The provider echoes the pseudonyms; Inbound must put the originals back
	// in both the text and the tool arguments.
	inner.reply.ToolCalls[0].Function.Arguments = `{"to":"<EMAIL_1>"}`
	inner.reply.Content = "sudah dikirim ke <EMAIL_1>"
	restored := redactor.Inbound(inner.reply)
	if !strings.Contains(restored.Content, "a@b.com") {
		t.Fatalf("content not restored: %q", restored.Content)
	}
	if !strings.Contains(restored.ToolCalls[0].Function.Arguments, "a@b.com") {
		t.Fatalf("tool arguments not restored: %q", restored.ToolCalls[0].Function.Arguments)
	}
}

func TestPseudonymNumberingIsDeterministic(t *testing.T) {
	patterns := emailAndPhone(t)
	first := New(patterns...)
	second := New(patterns...)
	messages := []ai.Message{{Role: "user", Content: "b@x.com lalu a@x.com"}}

	one := first.Outbound(messages)[0].Content
	two := second.Outbound(messages)[0].Content
	if one != two {
		t.Fatalf("numbering differs: %q vs %q", one, two)
	}
	if !strings.Contains(one, "<EMAIL_1>") || !strings.Contains(one, "<EMAIL_2>") {
		t.Fatalf("unexpected numbering: %q", one)
	}
}

func TestMappingIsACopy(t *testing.T) {
	redactor := New(emailAndPhone(t)...)
	redactor.Outbound([]ai.Message{{Role: "user", Content: "a@b.com"}})
	mapping := redactor.Mapping()
	if mapping["<EMAIL_1>"] != "a@b.com" {
		t.Fatalf("mapping = %v", mapping)
	}
	mapping["<EMAIL_1>"] = "tampered"
	if redactor.Mapping()["<EMAIL_1>"] != "a@b.com" {
		t.Fatal("Mapping must return a copy")
	}
}

func TestWithTransformHidesProviderTraffic(t *testing.T) {
	redactor := New(emailAndPhone(t)...)
	inner := &captureLLM{reply: ai.ChatResult{Content: "ok"}}
	if _, err := llm.WithTransform(inner, redactor).ChatStream(context.Background(), []ai.Message{{Role: "user", Content: "a@b.com"}}, ai.ChatOptions{}, func(ai.StreamDelta) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(inner.captured[0].Content, "a@b.com") {
		t.Fatalf("provider saw the original: %q", inner.captured[0].Content)
	}
}
