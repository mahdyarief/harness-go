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

// terminalResult returns the arguments and tool name of the first terminal
// tool that ran successfully in the batch. Dispatch validated them against its
// schema.
func terminalResult(tools services.Tools, calls []ai.ToolCall, records []services.ToolRecord) (json.RawMessage, string, bool) {
	for i, call := range calls {
		if i >= len(records) || records[i].Err != "" {
			continue
		}
		tool, ok := tools.Lookup(call.Function.Name)
		if !ok {
			continue
		}
		if terminal, ok := tool.(services.Terminal); ok && terminal.Terminal() {
			return json.RawMessage(call.Function.Arguments), call.Function.Name, true
		}
	}
	return nil, "", false
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
// arguments its schema rejected. Other failures (unknown tool, budget, denial)
// are not argument errors and must not consume a repair.
func requiredInvalid(records []services.ToolRecord, name string) bool {
	for _, record := range records {
		if record.Name == name && record.Err == codeToolArgsInvalid {
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
