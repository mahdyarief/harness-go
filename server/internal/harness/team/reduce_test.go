package team

import (
	"testing"

	"hsg/internal/harness/services"
)

func TestRulesFirstMatchReturnsName(t *testing.T) {
	rules := Rules[int]{
		{Name: "high", Match: func(results []MemberResult) bool { return len(results) > 0 && results[0].Outcome.PromptTokens > 10 }, Output: func([]MemberResult) int { return 3 }},
		{Name: "medium", Match: func(results []MemberResult) bool { return len(results) > 0 && results[0].Outcome.PromptTokens > 5 }, Output: func([]MemberResult) int { return 2 }},
		{Name: "low", Match: func([]MemberResult) bool { return true }, Output: func([]MemberResult) int { return 1 }},
	}
	got, name, ok := rules.Reduce([]MemberResult{{Outcome: services.Outcome{PromptTokens: 8}}})
	if !ok || got != 2 || name != "medium" {
		t.Fatalf("Reduce() = (%d, %q, %v), want (2, medium, true)", got, name, ok)
	}
}
