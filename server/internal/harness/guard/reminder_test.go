package guard_test

import (
	"strings"
	"testing"

	"hsg/internal/harness/guard"
)

func TestReminderConfigRejectsInvalidThresholds(t *testing.T) {
	tests := []struct {
		name    string
		cfg     guard.ReminderConfig
		wantErr string
	}{
		{"empty", guard.ReminderConfig{Thresholds: []int{}}, "must not be empty"},
		{"below two", guard.ReminderConfig{Thresholds: []int{1}}, "at least 2"},
		{"duplicate", guard.ReminderConfig{Thresholds: []int{3, 3}}, "duplicates"},
		{"non integer gap", guard.ReminderConfig{Thresholds: []int{2, 4, 3}}, "must be strictly increasing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := guard.NewReminderTool(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v; want %q", err, tt.wantErr)
			}
		})
	}
}

func TestReminderCanonicalizesArgumentKeyOrder(t *testing.T) {
	var g guard.ReminderTool
	cfg := guard.ReminderConfig{Thresholds: []int{2, 3}}
	var err error
	g, err = guard.NewReminderTool(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first := g.Observe("lookup", `{"a":1,"b":{"y":2,"x":1}}`)
	if first != nil {
		t.Fatalf("first = %q; want no reminder", first)
	}
	second := g.Observe("lookup", `{"b":{"x":1,"y":2},"a":1}`)
	if second == nil || !strings.Contains(second.Text, "repeating the exact same tool call") {
		t.Fatalf("second = %+v; want gentle reminder at first threshold", second)
	}
	third := g.Observe("lookup", `{"a":1,"b":{"y":2,"x":1}}`)
	if third == nil || !strings.Contains(third.Text, "consecutive_calls: 3") {
		t.Fatalf("third = %+v; want detailed reminder at second threshold", third)
	}
	if !strings.Contains(third.Text, `"a":1,"b":{"x":1,"y":2}`) {
		t.Fatalf("canonical args missing in %q", third.Text)
	}
}

func TestReminderIsAdvisoryAndResets(t *testing.T) {
	g, err := guard.NewReminderTool(guard.ReminderConfig{Thresholds: []int{2}})
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Observe("lookup", `{"q":"a"}`); got != nil {
		t.Fatalf("first = %+v", got)
	}
	g.Observe("lookup", `{"q":"a"}`)
	g.Observe("lookup", `{"q":"a"}`)
	if got := g.Observe("lookup", `{"q":"a"}`); got != nil {
		t.Fatalf("threshold reminder must fire once: %+v", got)
	}
	g.Reset()
	if got := g.Observe("lookup", `{"q":"a"}`); got != nil {
		t.Fatalf("reset must clear chain: %+v", got)
	}
}

func TestReminderWildcardIncludeExclude(t *testing.T) {
	g, err := guard.NewReminderTool(guard.ReminderConfig{
		Thresholds: []int{2},
		Include:    []string{"mcp_*"},
		Exclude:    []string{"mcp_secret_*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	g.Observe("lookup", `{}`)
	g.Observe("lookup", `{}`)
	if got := g.Observe("lookup", `{}`); got != nil {
		t.Fatalf("untracked tool produced reminder: %+v", got)
	}
	g.Observe("mcp_search", `{}`)
	if got := g.Observe("mcp_search", `{}`); got == nil {
		t.Fatal("included tool should produce reminder")
	}
	g2, _ := guard.NewReminderTool(guard.ReminderConfig{Thresholds: []int{2}, Exclude: []string{"mcp_secret_*"}})
	g2.Observe("mcp_secret_key", `{}`)
	g2.Observe("mcp_secret_key", `{}`)
	if got := g2.Observe("mcp_secret_key", `{}`); got != nil {
		t.Fatalf("excluded tool produced reminder: %+v", got)
	}
}

func TestReminderPreviewCapsArguments(t *testing.T) {
	g, err := guard.NewReminderTool(guard.ReminderConfig{Thresholds: []int{3, 4}, ArgumentsPreviewChars: 10})
	if err != nil {
		t.Fatal(err)
	}
	g.Observe("lookup", `{"q":"abcdefghijklmnop"}`)
	g.Observe("lookup", `{"q":"abcdefghijklmnop"}`)
	g.Observe("lookup", `{"q":"abcdefghijklmnop"}`)
	got := g.Observe("lookup", `{"q":"abcdefghijklmnop"}`)
	if got == nil || !strings.Contains(got.Text, "more chars") {
		t.Fatalf("got = %+v; want capped preview", got)
	}
}
