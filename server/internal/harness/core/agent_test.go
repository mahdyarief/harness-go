package core_test

import (
	"context"
	"testing"

	"hsg/internal/harness/core"
	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

type stubLoop struct{ called bool }

func (s *stubLoop) Run(context.Context, services.RunRequest, services.Sink) (services.Outcome, error) {
	s.called = true
	return services.Outcome{Content: "ok"}, nil
}

func TestFacadeDelegatesToLoop(t *testing.T) {
	s := scope.New(nil)
	loop := &stubLoop{}
	_ = s.Provide(services.AgentLoopKey, services.AgentLoop(loop), nil)
	a := core.NewAgent(s)
	out, err := a.Run(context.Background(), services.RunRequest{}, nil)
	if err != nil || out.Content != "ok" || !loop.called {
		t.Fatalf("out=%+v err=%v called=%v", out, err, loop.called)
	}
}
