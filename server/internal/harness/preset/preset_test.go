package preset_test

import (
	"testing"

	"hsg/internal/harness/preset"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

func TestApplyInstallsApprovalAndSandboxKeys(t *testing.T) {
	s := scope.New(nil)
	if err := preset.Apply(s, "read-only"); err != nil {
		t.Fatal(err)
	}
	if _, ok := scope.Require(s, services.ApprovalKey); !ok {
		t.Fatal("approval policy key not provided")
	}
	if _, ok := scope.Require(s, services.SandboxKey); !ok {
		t.Fatal("sandbox policy key not provided")
	}
}
