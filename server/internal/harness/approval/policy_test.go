package approval_test

import (
	"testing"

	"hsg/internal/harness/approval"
)

func TestDecideNeverAllowsAndAskRequestsApproval(t *testing.T) {
	never := approval.Decide(approval.Config{Policy: "never"}, "web_search")
	if !never.Allowed {
		t.Fatal("never policy must allow")
	}
	ask := approval.Decide(approval.Config{Policy: "ask"}, "web_search")
	if ask.Allowed || ask.Reason == "" {
		t.Fatalf("ask policy must require approval with reason, got %+v", ask)
	}
}
