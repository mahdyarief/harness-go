package llm_test

import (
	"context"
	"testing"

	"hsg/internal/harness/llm"
	"hsg/internal/lib/ai"
)

func TestAdapterReturnsConfigErrorForNilGateway(t *testing.T) {
	adapter := llm.New(nil)
	if adapter == nil {
		t.Fatal("adapter must not be nil")
	}
	_, err := adapter.ChatStream(context.Background(), nil, ai.ChatOptions{}, nil)
	if err == nil {
		t.Fatal("want error from nil gateway")
	}
}
