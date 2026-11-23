// Package agent is a small, provider-agnostic tool-calling harness. It owns the
// turn loop, budgets, context compaction and tool dispatch; the transport and
// all egress security stay in internal/lib/ai.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/google/uuid"

	"hsg/internal/lib/ai"
)

var ErrToolNotAllowed = errors.New("tool not in allow-list")
var ErrToolInput = errors.New("invalid tool input")

// Reference is the provenance of one knowledge passage a tool used. It is
// derived from the tool result, not from the model's text, so a citation
// always points at a real chunk.
type Reference struct {
	DocumentID  uuid.UUID `json:"document_id"`
	ChunkID     uuid.UUID `json:"chunk_id"`
	Title       string    `json:"title"`
	Filename    string    `json:"filename"`
	Format      string    `json:"format"`
	Locator     string    `json:"locator"`
	HeadingPath string    `json:"heading_path"`
	Position    int       `json:"position"`
	Excerpt     string    `json:"excerpt"`
	// Source records which retrieval branch produced the passage: "lexical",
	// "semantic", or "both". It is carried into the usage log so a cited event
	// keeps the embedding attribution of the retrieval that created it.
	Source string `json:"source,omitempty"`
	// Meta carries the document's retrieval metadata: attributes plus the
	// version label. It travels on every citation so the model can name the
	// regulation version it quoted without a second query.
	Meta map[string]any `json:"meta,omitempty"`
}

// ToolResult is what a tool returns. Output becomes the tool message the model
// reads; References are provenance records the runner surfaces to the caller so
// a citation always points at a real chunk rather than at model prose.
type ToolResult struct {
	Output     string
	References []Reference
}

// Tool is the only way the model can reach application data. Implementations
// must be read-only unless ReadOnly is false; the runner serializes writes.
type Tool interface {
	Name() string
	Description() string
	// Parameters is a JSON Schema object. It is validated against the model's
	// arguments before Execute runs.
	Parameters() json.RawMessage
	ReadOnly() bool
	Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

// Registry is an explicit allow-list. A tool the model invents is rejected
// before it can run, and the rejection is fed back as text.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range tools {
		r.tools[t.Name()] = t
	}
	return r
}

func (r *Registry) Lookup(name string) (Tool, bool) {
	if r == nil {
		return nil, false
	}
	t, ok := r.tools[name]
	return t, ok
}

// Specs returns the schemas in a stable order so prompts stay cacheable.
func (r *Registry) Specs() []ai.ToolSpec {
	if r == nil || len(r.tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	specs := make([]ai.ToolSpec, 0, len(names))
	for _, name := range names {
		t := r.tools[name]
		specs = append(specs, ai.ToolSpec{
			Type: "function",
			Function: ai.ToolFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return specs
}

// validateArgs checks the model's arguments against the declared schema at the
// level this harness can enforce without a JSON Schema dependency: the payload
// must be an object, contain every required key, and add no undeclared key.
// Types are left to the tool itself.
func validateArgs(schema, args json.RawMessage) error {
	var spec struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(schema, &spec); err != nil || spec.Type != "object" {
		return ErrToolInput
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal(args, &input); err != nil || input == nil {
		return ErrToolInput
	}
	for _, key := range spec.Required {
		if _, ok := input[key]; !ok {
			return ErrToolInput
		}
	}
	for key := range input {
		if _, ok := spec.Properties[key]; !ok {
			return ErrToolInput
		}
	}
	return nil
}

// truncate bounds a tool result so one call cannot blow the context budget.
// The marker tells the model the text was cut rather than the data ending.
func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	if cut > len(s) {
		cut = len(s)
	}
	return strings.ToValidUTF8(s[:cut], "") + "\n[truncated: result exceeded budget]"
}
