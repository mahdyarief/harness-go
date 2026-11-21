package harness_test

import (
	"testing"

	"hsg/internal/harness"
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
	if err := s.Dispose(); err != nil {
		t.Fatal(err)
	}
}
