package harness_test

import (
	"context"
	"testing"

	"hsg/internal/harness"
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
