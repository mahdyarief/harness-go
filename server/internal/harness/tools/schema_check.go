package tools

import (
	"fmt"

	"hsg/internal/harness/schema"
	"hsg/internal/harness/services"
)

// ValidateSchemas rejects a registry whose published tool schemas cannot be
// compiled, so a broken schema fails at mount instead of at every model call.
func ValidateSchemas(tools services.Tools) error {
	if tools == nil {
		return nil
	}
	for _, spec := range tools.Specs() {
		if _, err := schema.Compile(spec.Function.Parameters); err != nil {
			return fmt.Errorf("tool %q: %w", spec.Function.Name, err)
		}
	}
	return nil
}
