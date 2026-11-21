package sandbox_test

import (
	"strings"
	"testing"

	"hsg/internal/harness/sandbox"
)

func TestValidateDefaultsAndRejectsRelativeWorkspace(t *testing.T) {
	if err := sandbox.Validate(sandbox.Config{}); err != nil {
		t.Fatalf("empty config must default to read-only: %v", err)
	}
	cfg := sandbox.Config{Mode: "workspace-write", WorkspaceRoot: "relative/path"}
	err := sandbox.Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "absolut") {
		t.Fatalf("err=%v; want absolute-path error", err)
	}
}
