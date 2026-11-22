// Package evidence types the provenance of every claim a model makes. A tool
// returns Items for the records it actually read; the loop collects them into
// the run's Outcome and verifies that a structured submission only cites refs
// that appeared in a tool result of the same run. A ref the model invents is
// rejected with EVIDENCE_UNGROUNDED and fed back as a structured repair.
//
// Port of DSH deliverable-file-references: the reference is derived from the
// tool result, never from the model's text.
package evidence

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Item is one piece of provenance. Kind is a registered type name (DOCUMENT,
// RULE, GRAPH, SERVICE, RECORD, ...); Ref is the stable id of the cited thing
// (chunk id, rule id@version, edge id, record#field); Excerpt is the text the
// claim rests on; Meta carries kind-specific fields such as version, effective
// date or article number.
type Item struct {
	Kind    string            `json:"kind"`
	Ref     string            `json:"ref"`
	Excerpt string            `json:"excerpt,omitempty"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// Resolver checks that a Ref exists for its kind. It is optional: without one
// the registry still grounds refs against the run's collected items.
type Resolver func(ref string) error

// Registry records the valid kinds and, per kind, an optional Resolver.
// A tool returning an unregistered kind has a wiring bug, so Registry rejects
// it at collection time rather than letting the model cite it.
type Registry struct {
	kinds    map[string]bool
	resolver map[string]Resolver
}

func NewRegistry(kinds ...string) *Registry {
	r := &Registry{kinds: map[string]bool{}, resolver: map[string]Resolver{}}
	for _, k := range kinds {
		r.kinds[k] = true
	}
	return r
}

// Register adds a kind with an optional resolver.
func (r *Registry) Register(kind string, resolver Resolver) {
	r.kinds[kind] = true
	if resolver != nil {
		r.resolver[kind] = resolver
	}
}

func (r *Registry) Valid(kind string) bool { return r.kinds[kind] }

func (r *Registry) Resolve(kind, ref string) error {
	if !r.Valid(kind) {
		return fmt.Errorf("evidence: unregistered kind %q", kind)
	}
	if resolve, ok := r.resolver[kind]; ok {
		return resolve(ref)
	}
	return nil
}

// key is what Check compares: kind + ref. Excerpts and meta may be
// paraphrased by the model; the citation target may not.
func key(kind, ref string) string { return kind + "\x00" + ref }

func keys(items []Item) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[key(item.Kind, item.Ref)] = true
	}
	return set
}

// Citation is one evidence reference found in a structured submission.
type Citation struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

// Extract pulls every {kind, ref} pair out of a structured submission. It
// walks objects and arrays recursively, so the schema may nest citations
// under any field name as long as each object carries both keys.
func Extract(structured json.RawMessage) []Citation {
	var root any
	if err := json.Unmarshal(structured, &root); err != nil {
		return nil
	}
	var out []Citation
	var walk func(node any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			kind, kindOK := v["kind"].(string)
			ref, refOK := v["ref"].(string)
			if kindOK && refOK && kind != "" && ref != "" {
				out = append(out, Citation{Kind: kind, Ref: ref})
			}
			for _, value := range v {
				walk(value)
			}
		case []any:
			for _, value := range v {
				walk(value)
			}
		}
	}
	walk(root)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

// Ungrounded is the error fed back as a structured repair. It names every
// citation that no tool result in the run produced.
type Ungrounded struct {
	Missing []Citation
}

func (u *Ungrounded) Error() string {
	parts := make([]string, 0, len(u.Missing))
	for _, c := range u.Missing {
		parts = append(parts, c.Kind+":"+c.Ref)
	}
	return "evidence ungrounded: " + strings.Join(parts, ", ")
}

func (u *Ungrounded) Code() string { return "EVIDENCE_UNGROUNDED" }

// Check verifies that every evidence ref in the structured result at path
// appeared in a tool result of the same run. seen is the run's collected
// evidence. An unregistered kind is ungrounded too: the registry is the
// schema of what may be cited.
func Check(structured json.RawMessage, path string, registry *Registry, seen []Item) error {
	var node any
	if err := json.Unmarshal(structured, &node); err != nil {
		return nil
	}
	for _, part := range splitPath(path) {
		v, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		node, ok = v[part]
		if !ok {
			return nil
		}
	}
	payload, err := json.Marshal(node)
	if err != nil {
		return nil
	}
	citations := Extract(payload)
	if len(citations) == 0 {
		return nil
	}
	grounded := keys(seen)
	var missing []Citation
	for _, c := range citations {
		if registry != nil && !registry.Valid(c.Kind) {
			missing = append(missing, c)
			continue
		}
		if !grounded[key(c.Kind, c.Ref)] {
			missing = append(missing, c)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return &Ungrounded{Missing: missing}
}

func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(strings.Trim(path, "."), ".")
}
