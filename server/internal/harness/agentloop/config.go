package agentloop

import (
	"fmt"
	"time"

	"hsg/internal/harness/services"
)

func ResolveLoopConfig(c services.LoopConfig) (services.LoopConfig, error) {
	var errs []error
	checkInt := func(field string, value, def, min, max int) int {
		if value == 0 {
			return def
		}
		if value < min || value > max {
			errs = append(errs, fmt.Errorf("%s=%d out of range [%d,%d]", field, value, min, max))
			return value
		}
		return value
	}
	checkDur := func(field string, value, def, min, max time.Duration) time.Duration {
		if value == 0 {
			return def
		}
		if value < min || value > max {
			errs = append(errs, fmt.Errorf("%s=%s out of range [%s,%s]", field, value, min, max))
			return value
		}
		return value
	}

	c.MaxTurns = checkInt("MaxTurns", c.MaxTurns, 6, 1, 8)
	c.MaxToolCalls = checkInt("MaxToolCalls", c.MaxToolCalls, 8, 1, 16)
	c.MaxDuration = checkDur("MaxDuration", c.MaxDuration, 90*time.Second, time.Millisecond, 2*time.Minute)
	c.MaxTotalTokens = checkInt("MaxTotalTokens", c.MaxTotalTokens, 32768, 1, 32768)
	c.ToolResultBytes = checkInt("ToolResultBytes", c.ToolResultBytes, 8192, 1, 32768)
	c.MaxToolResultBytes = checkInt("MaxToolResultBytes", c.MaxToolResultBytes, 24576, 1, 65536)
	c.MaxToolConcurrency = checkInt("MaxToolConcurrency", c.MaxToolConcurrency, 4, 1, 8)
	c.ToolTimeout = checkDur("ToolTimeout", c.ToolTimeout, 15*time.Second, time.Millisecond, 30*time.Second)
	c.ContextTokenCap = checkInt("ContextTokenCap", c.ContextTokenCap, 12288, 1, 32768)
	c.KeepLastMessages = checkInt("KeepLastMessages", c.KeepLastMessages, 12, 1, 32)
	c.MaxRetries = checkInt("MaxRetries", c.MaxRetries, 1, 0, 3)
	c.MaxContinuationNudges = checkInt("MaxContinuationNudges", c.MaxContinuationNudges, 8, 0, 20)
	c.MaxPartialContinuations = checkInt("MaxPartialContinuations", c.MaxPartialContinuations, 2, 1, 8)
	c.MaxStructuredRepairs = checkInt("MaxStructuredRepairs", c.MaxStructuredRepairs, 2, 1, 8)

	if len(c.ReminderThresholds) == 0 {
		c.ReminderThresholds = []int{3, 5, 8}
	}
	if c.ReminderPreviewChars == 0 {
		c.ReminderPreviewChars = 500
	}

	if len(errs) > 0 {
		return c, fmt.Errorf("invalid loop config: %v", errs)
	}
	return c, nil
}
