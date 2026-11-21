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
		{"context in 413", 413, "prompt is too long", CodeContextWindow},
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
