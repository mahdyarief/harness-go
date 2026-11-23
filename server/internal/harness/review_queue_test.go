package harness_test

import (
	"context"
	"testing"

	"hsg/internal/harness"
	"hsg/internal/harness/evidence"
	"hsg/internal/harness/review"
	"hsg/internal/harness/services"
)

func TestQueueReviewHonoursPolicy(t *testing.T) {
	store := review.NewMemory()
	flagged := services.Outcome{Partial: true, Evidence: []evidence.Item{{Kind: "DOCUMENT", Ref: "doc:1"}}}

	item, queued, err := harness.QueueReview(context.Background(), store, func(out services.Outcome) bool {
		return out.Partial
	}, "run-1", "profil", "reviewer", flagged)
	if err != nil {
		t.Fatal(err)
	}
	if !queued {
		t.Fatal("partial result not queued")
	}
	if item.RunID != "run-1" || item.Reason == "" {
		t.Fatalf("item = %+v", item)
	}
	if len(item.Evidence) != 1 || item.Evidence[0].Ref != "doc:1" {
		t.Fatalf("evidence lost: %+v", item.Evidence)
	}
}

func TestQueueReviewSkipsWhenPolicySaysNo(t *testing.T) {
	store := review.NewMemory()
	_, queued, err := harness.QueueReview(context.Background(), store, func(services.Outcome) bool { return false },
		"run-1", "", "", services.Outcome{})
	if err != nil {
		t.Fatal(err)
	}
	if queued {
		t.Fatal("policy rejection still queued an item")
	}
}

func TestQueueReviewIsNoOpWithoutStore(t *testing.T) {
	_, queued, err := harness.QueueReview(context.Background(), nil, func(services.Outcome) bool { return true },
		"run-1", "", "", services.Outcome{})
	if err != nil || queued {
		t.Fatalf("queued=%v err=%v", queued, err)
	}
}
