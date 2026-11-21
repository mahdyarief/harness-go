package core

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRetainHeadTailUnderCap(t *testing.T) {
	in := "abcdef"
	if got := RetainHeadTail(in, 3, 3); got != in {
		t.Fatalf("got %q; want unchanged %q", got, in)
	}
}

func TestRetainHeadTailKeepsBothEndsAndCountsBytes(t *testing.T) {
	in := strings.Repeat("a", 20) + strings.Repeat("b", 20)
	got := RetainHeadTail(in, 5, 5)
	if !strings.HasPrefix(got, "aaaaa") || !strings.HasSuffix(got, "bbbbb") {
		t.Fatalf("got %q; want both ends", got)
	}
	if !strings.Contains(got, "[... tool result middle pruned ...]") {
		t.Fatalf("got %q; want marker", got)
	}
	if !strings.Contains(got, "30 bytes") {
		t.Fatalf("got %q; want omitted byte count", got)
	}
}

func TestRetainHeadTailNeverSplitsRune(t *testing.T) {
	in := strings.Repeat("é", 20)
	got := RetainHeadTail(in, 3, 3)
	if strings.Contains(got, "\ufffd") {
		t.Fatalf("got %q; want no replacement rune", got)
	}
	if !strings.HasPrefix(got, "ééé") || !strings.HasSuffix(got, "ééé") {
		t.Fatalf("got %q; want valid rune boundaries", got)
	}
}

func TestRetainHeadTailBytesRespectsByteBudget(t *testing.T) {
	in := strings.Repeat("é", 100) // 200 bytes
	got := RetainHeadTailBytes(in, 9, 5)
	if !utf8.ValidString(got) {
		t.Fatalf("got %q; invalid UTF-8", got)
	}
	head, tail, ok := strings.Cut(got, pruneMarker)
	if !ok {
		t.Fatalf("got %q; want marker", got)
	}
	if len(head) > 9 || head != "éééé" {
		t.Fatalf("head=%q (%d bytes); want 4 runes within 9 bytes", head, len(head))
	}
	kept := strings.TrimPrefix(tail, " (188 bytes omitted)")
	if kept == tail {
		t.Fatalf("tail=%q; want omitted byte count", tail)
	}
	if len(kept) > 5 || kept != "éé" {
		t.Fatalf("kept tail=%q; want 2 runes within 5 bytes", kept)
	}
}

func TestRetainHeadTailBytesUnderBudgetUnchanged(t *testing.T) {
	if got := RetainHeadTailBytes("abcdef", 3, 3); got != "abcdef" {
		t.Fatalf("got %q", got)
	}
}
