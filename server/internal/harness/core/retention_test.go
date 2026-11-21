package core

import (
	"strings"
	"testing"
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
