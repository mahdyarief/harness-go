package agentloop_test

import (
	"strings"
	"testing"
	"time"

	"hsg/internal/harness/agentloop"
	"hsg/internal/harness/services"
)

func TestResolveLoopConfigDefaultsZeroValues(t *testing.T) {
	got, err := agentloop.ResolveLoopConfig(services.LoopConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxTurns != 6 || got.MaxToolCalls != 8 || got.MaxDuration != 90*time.Second {
		t.Fatalf("defaults = %+v", got)
	}
}

func TestResolveLoopConfigKeepsValidExplicitValues(t *testing.T) {
	in := services.LoopConfig{MaxTurns: 3, MaxToolCalls: 2, MaxDuration: 10 * time.Second}
	got, err := agentloop.ResolveLoopConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxTurns != 3 || got.MaxToolCalls != 2 || got.MaxDuration != 10*time.Second {
		t.Fatalf("resolved = %+v; want explicit values preserved", got)
	}
}

func TestResolveLoopConfigRejectsOutOfRange(t *testing.T) {
	_, err := agentloop.ResolveLoopConfig(services.LoopConfig{MaxTurns: 99})
	if err == nil || !strings.Contains(err.Error(), "MaxTurns") {
		t.Fatalf("err = %v; want MaxTurns validation", err)
	}
	_, err = agentloop.ResolveLoopConfig(services.LoopConfig{ToolResultBytes: -1})
	if err == nil || !strings.Contains(err.Error(), "ToolResultBytes") {
		t.Fatalf("err = %v; want ToolResultBytes validation", err)
	}
}
