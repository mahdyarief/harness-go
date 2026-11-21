package agentloop

import "hsg/internal/harness/core"

// truncate bounds a tool result to limit bytes plus the prune marker, keeping
// head and tail in DSH's 4:1 ratio (compaction-tool-result-pruner 4096:1024).
func truncate(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	tail := max(1, limit/5)
	return core.RetainHeadTailBytes(s, limit-tail, tail)
}
