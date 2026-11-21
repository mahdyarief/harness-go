package harness_test

import (
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
