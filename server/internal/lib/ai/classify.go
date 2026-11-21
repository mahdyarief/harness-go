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

var (
	quotaPattern   = regexp.MustCompile(`(?i)(quota|billing|insufficient[_ ]?(credit|balance)|out of credit)`)
	contextPattern = regexp.MustCompile(`(?i)(context window|maximum context|token limit|too many tokens|prompt is too long|input is too long)`)
)

// ClassifyHTTP maps an HTTP status and a bounded detail string to a stable
// code. Order matters: a 400 whose body says "context window" is a context
// error, not an invalid request.
func ClassifyHTTP(status int, detail string) FailureCode {
	switch {
	case status == 401 || status == 403:
		return CodeAuth
	case quotaPattern.MatchString(detail) || status == 402:
		return CodeQuota
	case status == 429:
		return CodeRateLimit
	case contextPattern.MatchString(detail):
		return CodeContextWindow
	case status == 400 || status == 413:
		return CodeInvalidRequest
	case status >= 500:
		return CodeServer
	default:
		return HTTPCode(status)
	}
}
