package guard_test

import (
	"testing"

	"hsg/internal/harness/guard"
)

func TestRepeatToolFlagsExactRepeat(t *testing.T) {
	g := guard.NewRepeatTool(1)
	if decision := g.Check("web_search", `{"q":"a"}`); decision.Flagged {
		t.Fatal("first call with window 1 must not be flagged")
	}
	if decision := g.Check("web_search", `{"q":"a"}`); !decision.Flagged {
		t.Fatal("exact repeat must be flagged")
	}
	if decision := g.Check("web_search", `{"q":"b"}`); decision.Flagged {
		t.Fatal("different args must not be flagged")
	}
}
