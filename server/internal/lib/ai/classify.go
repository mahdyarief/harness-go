package ai

import (
	"fmt"
	"regexp"
)

// FailureCode is a stable, content-free classification of a provider failure.
// It must never embed a provider's response body.
type FailureCode string

const (
	CodeEmptyResponse  FailureCode = "EMPTY_RESPONSE"
	CodeRateLimit      FailureCode = "RATE_LIMIT"
	CodeServer         FailureCode = "SERVER"
	CodeTimeout        FailureCode = "TIMEOUT"
	CodeTransport      FailureCode = "TRANSPORT"
	CodeQuota          FailureCode = "QUOTA"
	CodeContextWindow  FailureCode = "CONTEXT_WINDOW_EXCEEDED"
	CodeAuth           FailureCode = "AUTH"
	CodeInvalidRequest FailureCode = "INVALID_REQUEST"
	CodeStreamClosed   FailureCode = "STREAM_CLOSED"
	CodeUnknown        FailureCode = "UNKNOWN"
)

// DefaultRetryableCodes mirrors the DSH llm retry policy's initial set.
var DefaultRetryableCodes = map[string]bool{
	string(CodeEmptyResponse): true,
	string(CodeRateLimit):     true,
	string(CodeServer):        true,
	string(CodeTimeout):       true,
	string(CodeTransport):     true,
}

func HTTPCode(status int) FailureCode {
	return FailureCode(fmt.Sprintf("HTTP_%d", status))
}

// Context-overflow wording, ported 1:1 from DSH
// packages/llm/llm/src/error.ts:111-148 (isContextWindowExceededError).
var contextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[^a-z0-9])context[\s_-](?:length|window)[\s_-](?:exceed(?:ed|s)?|overflow(?:ed)?|limit[\s_-]exceeded)(?:$|[^a-z0-9])`),
	regexp.MustCompile(`(?i)\b(?:maximum|max)(?:\s+(?:allowed|supported))?\s+context\s+(?:length|window)\b`),
	regexp.MustCompile(`(?i)\b(?:request|prompt|input|messages?)\s+(?:is\s+|are\s+)?too\s+(?:large|long)\s+for\s+(?:(?:this|the)\s+)?(?:model(?:'s)?\s+)?context(?:\s+window)?\b`),
	regexp.MustCompile(`(?i)\b(?:input|prompt|request)\s+(?:is\s+)?too\s+(?:long|large)\s+for\s+(?:this|the)\s+model\b`),
	regexp.MustCompile(`(?i)\b(?:input|prompt|request|messages?)\b.{0,40}\b(?:exceed(?:s|ed)?|overflows?|is\s+larger\s+than)\b.{0,40}\b(?:the\s+)?(?:model(?:'s)?\s+)?context(?:\s+(?:length|window))?\b`),
}

// Terminal quota wording (not a transient rate limit), ported 1:1 from DSH
// packages/llm/llm/src/error.ts:155-161 (isQuotaExceededError).
var quotaPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\binsufficient[\s_-]+(?:quota|balance|credits?)\b`),
	regexp.MustCompile(`(?i)\b(?:quota|usage[\s_-]+limit)[\s_-]+(?:exceeded|exhausted|reached)\b`),
	regexp.MustCompile(`(?i)\bexceed(?:ed|s)?[\s_-]+(?:(?:your|the)[\s_-]+)?(?:current[\s_-]+)?quota\b`),
	regexp.MustCompile(`(?i)\b(?:balance|credits?)[\s_-]+(?:exhausted|depleted)\b`),
	regexp.MustCompile(`(?i)\bout[\s_-]+of[\s_-]+(?:credits?|budget)\b`),
}

// IsContextWindowExceeded reports DSH context-overflow wording in detail.
func IsContextWindowExceeded(detail string) bool { return matchAny(contextPatterns, detail) }

// IsQuotaExceeded reports DSH terminal-quota wording in detail.
func IsQuotaExceeded(detail string) bool { return matchAny(quotaPatterns, detail) }

func matchAny(patterns []*regexp.Regexp, detail string) bool {
	for _, p := range patterns {
		if p.MatchString(detail) {
			return true
		}
	}
	return false
}

// ClassifyHTTP maps an HTTP status and a bounded detail string to a stable
// code. Order matters: a 400 whose body says "context window" is a context
// error, not an invalid request.
func ClassifyHTTP(status int, detail string) FailureCode {
	switch {
	case status == 401 || status == 403:
		return CodeAuth
	case IsQuotaExceeded(detail) || status == 402:
		return CodeQuota
	case status == 429:
		return CodeRateLimit
	case IsContextWindowExceeded(detail):
		return CodeContextWindow
	case status == 400 || status == 413:
		return CodeInvalidRequest
	case status >= 500:
		return CodeServer
	default:
		return HTTPCode(status)
	}
}
