package llm

import (
	"context"
	"errors"
	"testing"

	"hsg/internal/lib/ai"
)

func TestFailureCodeClassifiesGatewayErrors(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		code      string
		retryable bool
	}{
		{"provider status", &ai.ProviderError{Status: 429, Code: "RATE_LIMIT"}, "RATE_LIMIT", true},
		{"transport", &ai.ProviderError{Code: "TRANSPORT"}, "TRANSPORT", true},
		{"gateway deadline", context.DeadlineExceeded, "TIMEOUT", true},
		{"stream read failure", ai.ErrProvider, "TRANSPORT", true},
		{"truncated stream", ai.ErrOutput, "STREAM_CLOSED", false},
		{"invalid input", ai.ErrInput, "UNKNOWN", false},
		{"bad config", ai.ErrConfig, "UNKNOWN", false},
		{"too large", ai.ErrResponseTooLarge, "UNKNOWN", false},
		{"client gone", context.Canceled, "UNKNOWN", false},
		{"other", errors.New("boom"), "UNKNOWN", false},
	}
	policy := DefaultRetryPolicy()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := failureCode(tc.err)
			if code != tc.code {
				t.Fatalf("code=%q want %q", code, tc.code)
			}
			wrapped := NewFailure("provider request failed", code, tc.err)
			if got := policy.Retryable(wrapped); got != tc.retryable {
				t.Fatalf("retryable=%v want %v", got, tc.retryable)
			}
			if !errors.Is(wrapped, tc.err) {
				t.Fatal("wrapping must keep the cause chain for handler mapping")
			}
		})
	}
}
