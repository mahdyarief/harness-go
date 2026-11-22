package rules

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"hsg/internal/harness/evidence"
	"hsg/internal/harness/services"
)

// Tool is the generic rule_check tool. It evaluates the loaded ruleset
// against a subject with no model in the loop, so the same subject always
// yields the same hits; every fired hit's evidence is attached to the
// result.
type Tool struct {
	engine *Engine
	now    func() time.Time
}

// NewTool wraps a loaded engine. now is injectable so experiments can pin
// the evaluation instant; nil means time.Now.
func NewTool(engine *Engine, now func() time.Time) *Tool {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Tool{engine: engine, now: now}
}

func (t *Tool) Name() string { return "rule_check" }

func (t *Tool) Description() string {
	return "Evaluasi aturan deklaratif terhadap subjek tanpa LLM. Mengembalikan aturan yang menyala beserta buktinya."
}

func (t *Tool) Parameters() json.RawMessage {
	groups := t.engine.Groups()
	enum := make([]any, len(groups))
	for i, g := range groups {
		enum[i] = g
	}
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subject": map[string]any{"type": "object"},
			"group":   map[string]any{"type": "string", "enum": enum},
		},
		"required": []string{"subject", "group"},
	}
	raw, _ := json.Marshal(params)
	return raw
}

func (t *Tool) ReadOnly() bool { return true }

type ruleCheckArgs struct {
	Subject json.RawMessage `json:"subject"`
	Group   string          `json:"group"`
}

// Execute evaluates the rules and returns the fired hits as JSON. A group
// with no rules is an error, not an empty success: a typo must not look
// like a clean bill of health.
func (t *Tool) Execute(_ context.Context, args json.RawMessage) (services.ToolResult, error) {
	var in ruleCheckArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return services.ToolResult{}, err
	}
	if len(t.engine.byGroup[in.Group]) == 0 {
		return services.ToolResult{}, errors.New("grup aturan tidak dikenal: " + in.Group)
	}
	hits := t.engine.Evaluate(in.Subject, in.Group, t.now())
	var ev []evidence.Item
	for _, hit := range hits {
		if hit.Fired {
			ev = append(ev, hit.Evidence...)
		}
	}
	out, err := json.Marshal(map[string]any{"group": in.Group, "hits": hits})
	if err != nil {
		return services.ToolResult{}, err
	}
	return services.ToolResult{Output: string(out), Evidence: ev}, nil
}
