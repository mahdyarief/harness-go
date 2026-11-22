package agentloop

import (
	"encoding/json"

	"hsg/internal/harness/services"
	"hsg/internal/lib/ai"
)

const (
	CodeStructuredMissing = "STRUCTURED_MISSING"
	CodeStructuredInvalid = "STRUCTURED_INVALID"
)

const structuredNudge = "Finish by calling the required tool with arguments that satisfy its schema."

// terminalCall returns the name of the first terminal tool in the batch.
func terminalCall(tools services.Tools, calls []ai.ToolCall) (string, bool) {
	for _, call := range calls {
		tool, ok := tools.Lookup(call.Function.Name)
		if !ok {
			continue
		}
		terminal, ok := tool.(services.Terminal)
		if ok && terminal.Terminal() {
			return call.Function.Name, true
		}
	}
	return "", false
}

func terminalResult(calls []ai.ToolCall, records []services.ToolRecord, name string) (json.RawMessage, bool) {
	for i, call := range calls {
		if i < len(records) && call.Function.Name == name && records[i].Err == "" {
			return json.RawMessage(call.Function.Arguments), true
		}
	}
	return nil, false
}

// requiredSubmission returns arguments from a successfully executed required
// tool. It need not have the optional Terminal marker.
func requiredSubmission(tools services.Tools, calls []ai.ToolCall, records []services.ToolRecord, name string) (json.RawMessage, bool) {
	if name == "" {
		return nil, false
	}
	for i, call := range calls {
		if i >= len(records) || call.Function.Name != name || records[i].Err != "" {
			continue
		}
		if _, ok := tools.Lookup(name); ok {
			return json.RawMessage(call.Function.Arguments), true
		}
	}
	return nil, false
}

// requiredInvalid reports whether the batch called the required tool with
// arguments its schema rejected.
func requiredInvalid(records []services.ToolRecord, name string) bool {
	for _, record := range records {
		if record.Name == name && record.Err != "" {
			return true
		}
	}
	return false
}

func structuredMissing() error {
	return services.NewHarnessError("model finished without the required structured submission", CodeStructuredMissing, nil)
}

func structuredInvalid() error {
	return services.NewHarnessError("structured submission arguments stayed invalid", CodeStructuredInvalid, nil)
}
