package repositories

import (
	"strings"
	"testing"
)

// knowledgeStatus keeps callers that predate the lifecycle (seeds, integration
// fixtures) on the published behaviour.
func TestKnowledgeStatusDefault(t *testing.T) {
	if got := knowledgeStatus(""); got != "published" {
		t.Fatalf("knowledgeStatus empty = %q, want published", got)
	}
	if got := knowledgeStatus("draft"); got != "draft" {
		t.Fatalf("knowledgeStatus draft = %q, want draft", got)
	}
}

// Draft documents must never reach the agent. Both hybrid branches (lexical and
// semantic) and both scope partitions carry the status gate.
func TestHybridCandidateSQLGatesDraft(t *testing.T) {
	for _, tenant := range []bool{false, true} {
		want := 2
		if tenant {
			want = 4
		}
		if got := strings.Count(hybridCandidateSQL(tenant), "d.status='published'"); got != want {
			t.Errorf("hybridCandidateSQL(tenant=%v): %d status gates, want %d", tenant, got, want)
		}
	}
}
