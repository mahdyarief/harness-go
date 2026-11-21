package agentloop

import "hsg/internal/harness/core"

func truncate(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	head := limit / 2
	return core.RetainHeadTail(s, head, limit-head)
}
