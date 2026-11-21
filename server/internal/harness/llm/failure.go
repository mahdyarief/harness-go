package llm

import (
	"errors"

	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

// NewFailure wraps a gateway error with a stable audit code. The wrapped
// message is content-free by construction: callers must pass a generic label.
func NewFailure(message, code string, err error) error {
	return services.NewHarnessError(message, code, err)
}

func FailureCodeOf(err error) string {
	if code := services.ErrorCode(err); code != "" {
		return code
	}
	var provider *ai.ProviderError
	if errors.As(err, &provider) {
		return provider.Code
	}
	return ""
}

// Failure codes are defined in lib/ai so the transport can classify them
// without importing the harness. Re-exported here for harness consumers.
type FailureCode = ai.FailureCode

const (
	CodeEmptyResponse  = ai.CodeEmptyResponse
	CodeRateLimit      = ai.CodeRateLimit
	CodeServer         = ai.CodeServer
	CodeTimeout        = ai.CodeTimeout
	CodeTransport      = ai.CodeTransport
	CodeQuota          = ai.CodeQuota
	CodeContextWindow  = ai.CodeContextWindow
	CodeAuth           = ai.CodeAuth
	CodeInvalidRequest = ai.CodeInvalidRequest
	CodeStreamClosed   = ai.CodeStreamClosed
	CodeUnknown        = ai.CodeUnknown
)

var DefaultRetryableCodes = ai.DefaultRetryableCodes

func HTTPCode(status int) FailureCode { return ai.HTTPCode(status) }
func ClassifyHTTP(status int, detail string) FailureCode {
	return ai.ClassifyHTTP(status, detail)
}
