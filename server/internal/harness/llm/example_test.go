package llm

import (
	"context"
	"fmt"

	"hsg/internal/harness/redact"
	"hsg/internal/lib/ai"
)

// echoLLM repeats the request text and echoes the pseudonym into a tool-call
// argument, the way a provider would reuse an identifier it was given.
type echoLLM struct{ seen string }

func (e *echoLLM) MaxTokens() int { return 100 }

func (e *echoLLM) ChatStream(_ context.Context, messages []ai.Message, _ ai.ChatOptions, _ func(ai.StreamDelta) error) (ai.ChatResult, error) {
	e.seen = messages[0].Content
	return ai.ChatResult{
		Content: e.seen,
		ToolCalls: []ai.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: ai.ToolCallFunction{
				Name:      "kirim",
				Arguments: `{"email":"` + e.seen[len("hubungi "):] + `"}`,
			},
		}},
	}, nil
}

// ExampleWithTransform shows the redaction contract: the provider never sees
// the original identifier, and both the text and the tool-call arguments are
// restored before the loop or a tool reads them.
func ExampleWithTransform() {
	pattern, err := redact.NewPattern("EMAIL", `[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	if err != nil {
		panic(err)
	}
	inner := &echoLLM{}
	client := WithTransform(inner, redact.New(pattern))
	result, err := client.ChatStream(context.Background(), []ai.Message{
		{Role: "user", Content: "hubungi budi@example.com"},
	}, ai.ChatOptions{}, nil)
	if err != nil {
		panic(err)
	}
	fmt.Println("outbound:", inner.seen)
	fmt.Println("restored text:", result.Content)
	fmt.Println("restored args:", result.ToolCalls[0].Function.Arguments)
	// Output:
	// outbound: hubungi <EMAIL_1>
	// restored text: hubungi budi@example.com
	// restored args: {"email":"budi@example.com"}
}
