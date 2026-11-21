package core

import (
	"context"

	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

type agentFacade struct{ scope *scope.Scope }

func NewAgent(s *scope.Scope) services.Agent { return &agentFacade{scope: s} }

func (a *agentFacade) Run(ctx context.Context, req services.RunRequest, sink services.Sink) (services.Outcome, error) {
	loop, ok := scope.Require(a.scope, services.AgentLoopKey)
	if !ok {
		return services.Outcome{}, services.ErrLoopMissing
	}
	return loop.Run(ctx, req, sink)
}
