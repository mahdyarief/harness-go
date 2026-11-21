package preset

import (
	"errors"

	"hsg/internal/harness/scope"
	"hsg/internal/harness/services"
)

var ErrUnknown = errors.New("preset tidak dikenal")

type Spec struct {
	SandboxMode   string
	Approval      string
	ToolAllowlist []string
}

var presets = map[string]Spec{
	"read-only": {SandboxMode: "read-only", Approval: "never"},
	"workspace": {SandboxMode: "workspace-write", Approval: "never"},
	"strict":    {SandboxMode: "read-only", Approval: "ask"},
}

func Get(name string) (Spec, bool) {
	spec, ok := presets[name]
	return spec, ok
}

func Apply(s *scope.Scope, name string) error {
	spec, ok := presets[name]
	if !ok {
		return ErrUnknown
	}
	if err := s.Provide(services.SandboxKey, spec.SandboxMode, nil); err != nil {
		return err
	}
	if err := s.Provide(services.ApprovalKey, spec.Approval, nil); err != nil {
		return err
	}
	return s.Provide(services.ToolAllowlistKey, spec.ToolAllowlist, nil)
}
