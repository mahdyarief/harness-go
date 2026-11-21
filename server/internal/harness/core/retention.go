package core

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const pruneMarker = "[... tool result middle pruned ...]"

// RetainHeadTail keeps the first head runes and last tail runes of s, joined
// by a stable marker that records the number of omitted bytes.
func RetainHeadTail(s string, head, tail int) string {
	if head <= 0 || tail <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= head+tail {
		return s
	}
	h := string(r[:head])
	t := string(r[len(r)-tail:])
	omitted := len(s) - len(h) - len(t)
	return h + pruneMarker + fmt.Sprintf(" (%d bytes omitted)", omitted) + t
}

// RuneSafePrefix returns at most n bytes without ending mid-rune.
func RuneSafePrefix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// RuneSafeSuffix returns at most n bytes without starting mid-rune.
func RuneSafeSuffix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(s) {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return strings.TrimPrefix(s[start:], "")
}
