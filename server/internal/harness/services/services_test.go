package services_test

import (
	"context"
	"testing"

	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

type fakeAgent struct{}

func (fakeAgent) Run(context.Context, services.RunRequest, services.Sink) (services.Outcome, error) {
	return services.Outcome{}, nil
}

func TestAgentKeyResolvesAgent(t *testing.T) {
	s := scope.New(nil)
	if err := s.Provide(services.AgentKey, fakeAgent{}, nil); err != nil {
		t.Fatal(err)
	}
	agent, ok := scope.Require(s, services.AgentKey)
	if !ok || agent == nil {
		t.Fatal("AgentKey did not resolve")
	}
}
