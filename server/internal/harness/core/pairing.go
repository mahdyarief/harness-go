package core

import (
	"fmt"

	"hsg/internal/lib/ai"
)

// ToolPairingBalanced reports whether the first cut messages of history have
// every assistant tool call answered by a following tool result. A tool result
// with no outstanding call is a hard error, not a silent imbalance.
func ToolPairingBalanced(history []ai.Message, cut int) (bool, error) {
	if cut < 0 || cut > len(history) {
		return false, fmt.Errorf("invalid cut %d for history of %d", cut, len(history))
	}
	pending := 0
	for _, message := range history[:cut] {
		switch message.Role {
		case "assistant":
			pending += len(message.ToolCalls)
		case "tool":
			if pending == 0 {
				return false, fmt.Errorf("orphan tool result in first %d messages", cut)
			}
			pending--
		}
	}
	return pending == 0, nil
}
