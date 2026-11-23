package harness_test

import (
	"context"
	"testing"
	"time"

	"hsg/internal/harness"
	"hsg/internal/harness/review"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
	"hsg/internal/lib/agent"
)

func TestMountRegistersServices(t *testing.T) {
	s, err := harness.Mount(nil, agent.NewRegistry(), harness.MountConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("nil scope")
	}
	if mode, _ := scope.Require(s, services.SandboxKey); mode != "read-only" {
		t.Fatalf("default sandbox = %q; want read-only", mode)
	}
	if policy, _ := scope.Require(s, services.ApprovalKey); policy != "never" {
		t.Fatalf("default approval = %q; want never", policy)
	}
	if err := s.Dispose(); err != nil {
		t.Fatal(err)
	}
}

func TestMountProvidesApprovalAnswer(t *testing.T) {
	answer := services.ApprovalAnswer(func(context.Context, services.ToolInvocation) (bool, error) { return true, nil })
	s, err := harness.Mount(nil, agent.NewRegistry(), harness.MountConfig{Preset: "strict", ApprovalAnswer: answer})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := scope.Require(s, services.ApprovalAnswerKey); !ok || got == nil {
		t.Fatal("approval answerer not provided")
	}
}

func TestMountActivatesEvidenceGrounding(t *testing.T) {
	s, err := harness.Mount(nil, agent.NewRegistry(), harness.MountConfig{
		EvidenceKinds: []string{"DOCUMENT", "RULE"},
		EvidencePath:  "evidence",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := scope.Require(s, services.LoopConfigKey)
	if !ok {
		t.Fatal("loop config not provided")
	}
	if cfg.EvidenceRegistry == nil {
		t.Fatal("evidence registry not activated")
	}
	if cfg.EvidencePath != "evidence" {
		t.Fatalf("evidence path = %q", cfg.EvidencePath)
	}
	if !cfg.EvidenceRegistry.Valid("DOCUMENT") || !cfg.EvidenceRegistry.Valid("RULE") {
		t.Fatal("declared kinds rejected")
	}
	if cfg.EvidenceRegistry.Valid("UNKNOWN") {
		t.Fatal("unknown kind accepted")
	}
}

func TestMountQueuesFlaggedOutcomeForReview(t *testing.T) {
	store := review.NewMemory()
	s, err := harness.Mount(nil, agent.NewRegistry(), harness.MountConfig{
		ReviewStore:  store,
		ReviewPolicy: func(out services.Outcome) bool { return out.Partial },
		RunID:        "run-1",
		ReviewRole:   "lead",
	})
	if err != nil {
		t.Fatal(err)
	}
	queue, ok := scope.Require(s, services.ReviewSinkKey)
	if !ok || queue == nil {
		t.Fatal("review sink not provided")
	}
	if err := queue(context.Background(), services.Outcome{Content: "draft"}); err != nil {
		t.Fatal(err)
	}
	if items, total, _ := store.List(context.Background(), "", 10, 0); total != 0 || len(items) != 0 {
		t.Fatalf("unflagged outcome queued: %d items", total)
	}
	if err := queue(context.Background(), services.Outcome{Content: "draft", Partial: true}); err != nil {
		t.Fatal(err)
	}
	items, total, err := store.List(context.Background(), "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("queued items = %d; want 1", total)
	}
	if items[0].RunID != "run-1" || items[0].Role != "lead" {
		t.Fatalf("queued item = %+v", items[0])
	}
	if len(items[0].Result) == 0 || items[0].Reason == "" {
		t.Fatalf("queued item lacks result or reason: %+v", items[0])
	}
}

func TestMountCarriesApprovalWaitTimeout(t *testing.T) {
	s, err := harness.Mount(nil, agent.NewRegistry(), harness.MountConfig{
		LoopConfig: services.LoopConfig{ApprovalMode: "wait", ApprovalTimeout: 3 * time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := scope.Require(s, services.LoopConfigKey)
	if cfg.ApprovalMode != "wait" || cfg.ApprovalTimeout != 3*time.Minute {
		t.Fatalf("approval config dropped: %q %s", cfg.ApprovalMode, cfg.ApprovalTimeout)
	}
}
