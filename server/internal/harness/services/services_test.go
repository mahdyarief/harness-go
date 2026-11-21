package services_test

import (
	"context"
	"errors"
	"testing"

	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

type fakeAgent struct{}

func (fakeAgent) Run(context.Context, services.RunRequest, services.Sink) (services.Outcome, error) {
	return services.Outcome{}, nil
}

func TestHarnessErrorCodeAndCause(t *testing.T) {
	cause := errors.New("root")
	err := services.NewHarnessError("tool timed out", "TOOL_TIMEOUT", cause)
	if err.Code != "TOOL_TIMEOUT" {
		t.Fatalf("Code=%q", err.Code)
	}
	if err.Error() != "tool timed out" {
		t.Fatalf("Error()=%q", err.Error())
	}
	if !errors.Is(err.Unwrap(), cause) {
		t.Fatal("cause not chained")
	}
}

func TestHarnessErrorCodeRouteWithoutMessageParsing(t *testing.T) {
	err := services.NewHarnessError("anything", "NO_ADAPTER", nil)
	var target *services.HarnessError
	if !errors.As(err, &target) || target.Code != "NO_ADAPTER" {
		t.Fatal("HarnessError not routable by code")
	}
}

func TestErrorCodeRoutesWrappedHarnessError(t *testing.T) {
	inner := services.NewHarnessError("timeout", "TOOL_TIMEOUT", nil)
	wrapped := errors.Join(errors.New("outer"), inner)
	if got := services.ErrorCode(wrapped); got != "TOOL_TIMEOUT" {
		t.Fatalf("ErrorCode=%q", got)
	}
	if got := services.ErrorCode(errors.New("plain")); got != "" {
		t.Fatalf("ErrorCode for plain error=%q", got)
	}
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
