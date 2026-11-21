package ai

import (
	"strings"
	"testing"
)

func TestClassifyHTTP(t *testing.T) {
	cases := []struct {
		name   string
		status int
		detail string
		want   FailureCode
	}{
		{"unauthorized", 401, "", CodeAuth},
		{"forbidden", 403, "", CodeAuth},
		{"quota wording", 400, "you exceeded your quota", CodeQuota},
		{"payment required", 402, "", CodeQuota},
		{"rate limit", 429, "", CodeRateLimit},
		{"context in 400", 400, "this model's maximum context length is exceeded", CodeContextWindow},
		{"context in 413", 413, "prompt is too long for the model", CodeContextWindow},
		{"bare too long is not context (DSH)", 413, "prompt is too long", CodeInvalidRequest},
		{"structured context code", 400, "context_length_exceeded", CodeContextWindow},
		{"billing word in 5xx is not quota", 503, "billing service unavailable", CodeServer},
		{"max_tokens param is not context", 400, "max_tokens exceeds the token limit", CodeInvalidRequest},
		{"rate limit with quota noun", 429, "rate limit quota per minute", CodeRateLimit},
		{"bad request", 400, "invalid json field", CodeInvalidRequest},
		{"payload too large", 413, "increase plan", CodeInvalidRequest},
		{"server", 503, "", CodeServer},
		{"other", 418, "", HTTPCode(418)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyHTTP(tc.status, tc.detail); got != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestDefaultRetryableCodes(t *testing.T) {
	for _, code := range []FailureCode{CodeEmptyResponse, CodeRateLimit, CodeServer, CodeTimeout, CodeTransport} {
		if !DefaultRetryableCodes[string(code)] {
			t.Errorf("%s must be retryable", code)
		}
	}
	for _, code := range []FailureCode{CodeAuth, CodeQuota, CodeContextWindow, CodeInvalidRequest} {
		if DefaultRetryableCodes[string(code)] {
			t.Errorf("%s must not be retryable", code)
		}
	}
}

func TestProviderErrorNeverLeaksDetail(t *testing.T) {
	secret := "sk-1234567890-VERY-SECRET"
	code := ClassifyHTTP(400, "your quota for "+secret+" is gone")
	err := &ProviderError{Status: 400, Code: string(code)}
	if got := err.Error(); strings.Contains(got, secret) || strings.Contains(got, "quota") {
		t.Fatalf("error leaked provider detail: %q", got)
	}
}

// Cases mirror DSH packages/llm/llm/src/error.ts classifier behaviour.
func TestContextWindowWording(t *testing.T) {
	for _, detail := range []string{
		"context length exceeded",
		"context_window_exceeded",
		"maximum context window",
		"max supported context length",
		"prompt is too long for the model",
		"request is too large for this model's context window",
		"input exceeds the model's context window",
	} {
		if !IsContextWindowExceeded(detail) {
			t.Errorf("not classified as context overflow: %q", detail)
		}
	}
	for _, detail := range []string{"request timeout", "token limit", "prompt is too long", "context"} {
		if IsContextWindowExceeded(detail) {
			t.Errorf("false positive: %q", detail)
		}
	}
}

func TestQuotaWording(t *testing.T) {
	for _, detail := range []string{
		"insufficient quota", "insufficient_balance", "quota exceeded",
		"usage limit reached", "you exceeded your current quota",
		"balance depleted", "credits exhausted", "out of credits", "out of budget",
	} {
		if !IsQuotaExceeded(detail) {
			t.Errorf("not classified as quota: %q", detail)
		}
	}
	for _, detail := range []string{"rate limit reached", "billing", "quota"} {
		if IsQuotaExceeded(detail) {
			t.Errorf("false positive: %q", detail)
		}
	}
}
