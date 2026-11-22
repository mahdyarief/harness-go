package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"hsg/internal/harness/evidence"
	"hsg/internal/harness/schema"
)

// Rule is one declarative check. It is data, not code: the expression is
// checked against the subject schema at load time and can never reach I/O.
type Rule struct {
	ID        string     `json:"id" yaml:"id"`
	Version   string     `json:"version" yaml:"version"`
	Group     string     `json:"group" yaml:"group"`
	Severity  string     `json:"severity" yaml:"severity"`
	When      string     `json:"when" yaml:"when"`
	Message   string     `json:"message" yaml:"message"`
	Source    string     `json:"source" yaml:"source"`
	Effective *Effective `json:"effective,omitempty" yaml:"effective,omitempty"`

	parsed node
	reads  []string
	line   int
	tmpl   *template.Template
}

// Effective windows a rule version: inclusive on both ends. A nil bound is
// open.
type Effective struct {
	From *string `json:"from,omitempty" yaml:"from,omitempty"`
	To   *string `json:"to,omitempty" yaml:"to,omitempty"`
}

// Hit is one evaluated rule. Evidence cites the rule version plus every
// field the expression read, so a fired rule is auditable to its inputs.
type Hit struct {
	RuleID   string          `json:"rule_id"`
	Version  string          `json:"version"`
	Severity string          `json:"severity"`
	Fired    bool            `json:"fired"`
	Message  string          `json:"message"`
	Evidence []evidence.Item `json:"evidence"`
}

// Engine holds loaded rules for one subject schema. It is immutable after
// Load and safe for concurrent Evaluate calls.
type Engine struct {
	subject *schema.Schema
	env     TypeEnv
	byGroup map[string][]*Rule
}

// Load compiles every rule against the subject schema. It fails on the first
// invalid rule, reporting the rule's source line, so a bad ruleset never
// starts. Rules are grouped and sorted for deterministic evaluation.
func Load(subject json.RawMessage, rules []Rule) (*Engine, error) {
	compiled, err := schema.Compile(subject)
	if err != nil {
		return nil, fmt.Errorf("skema subjek: %w", err)
	}
	env, err := typeEnv(compiled)
	if err != nil {
		return nil, err
	}
	e := &Engine{subject: compiled, env: env, byGroup: map[string][]*Rule{}}
	for i := range rules {
		r := &rules[i]
		if r.line == 0 {
			r.line = i + 1
		}
		if r.ID == "" || r.Version == "" || r.Group == "" || r.When == "" {
			return nil, fmt.Errorf("aturan baris %d: id, version, group, when wajib ada", r.line)
		}
		n, reads, err := Parse(r.When, env)
		if err != nil {
			return nil, fmt.Errorf("aturan %s@%s (baris %d): %w", r.ID, r.Version, r.line, err)
		}
		r.parsed = n
		r.reads = reads
		if r.Message != "" {
			t, err := template.New(r.ID).Parse(r.Message)
			if err != nil {
				return nil, fmt.Errorf("aturan %s@%s (baris %d): template pesan: %w", r.ID, r.Version, r.line, err)
			}
			r.tmpl = t
		}
		e.byGroup[r.Group] = append(e.byGroup[r.Group], r)
	}
	for group := range e.byGroup {
		sort.Slice(e.byGroup[group], func(i, j int) bool {
			a, b := e.byGroup[group][i], e.byGroup[group][j]
			if a.ID != b.ID {
				return a.ID < b.ID
			}
			return a.Version < b.Version
		})
	}
	return e, nil
}

// Evaluate runs every rule in the group whose effective window contains at.
// It never calls a model: the same subject, group, and instant always
// produce byte-identical hits.
func (e *Engine) Evaluate(subject json.RawMessage, group string, at time.Time) []Hit {
	dec := json.NewDecoder(bytes.NewReader(subject))
	dec.UseNumber()
	var fields map[string]any
	if err := dec.Decode(&fields); err != nil || fields == nil {
		fields = map[string]any{}
	}
	day := int64(at.Unix() / 86400)
	tv := timeValue{day: day}

	var hits []Hit
	for _, r := range e.byGroup[group] {
		if !effectiveAt(r.Effective, at) {
			continue
		}
		v, err := r.parsed.eval(fields, tv)
		if err != nil {
			// A missing field at runtime is not a fire; exists() is the
			// explicit way to ask. Other errors fail the rule closed.
			hits = append(hits, Hit{RuleID: r.ID, Version: r.Version, Severity: r.Severity, Fired: false})
			continue
		}
		hit := Hit{RuleID: r.ID, Version: r.Version, Severity: r.Severity, Fired: v.kind == KBool && v.b}
		if hit.Fired {
			hit.Message = render(r, fields)
			hit.Evidence = ruleEvidence(r, e, fields)
		}
		hits = append(hits, hit)
	}
	return hits
}

// Groups returns the loaded group names, sorted.
func (e *Engine) Groups() []string {
	out := make([]string, 0, len(e.byGroup))
	for group := range e.byGroup {
		out = append(out, group)
	}
	sort.Strings(out)
	return out
}

func effectiveAt(w *Effective, at time.Time) bool {
	if w == nil {
		return true
	}
	day := at.Format("2006-01-02")
	if w.From != nil && day < *w.From {
		return false
	}
	if w.To != nil && day > *w.To {
		return false
	}
	return true
}

func render(r *Rule, fields map[string]any) string {
	if r.tmpl == nil {
		return ""
	}
	var b strings.Builder
	if err := r.tmpl.Execute(&b, fields); err != nil {
		return ""
	}
	return b.String()
}

func ruleEvidence(r *Rule, e *Engine, fields map[string]any) []evidence.Item {
	meta := map[string]string{"severity": r.Severity, "group": r.Group}
	if r.Source != "" {
		meta["source"] = r.Source
	}
	if r.Effective != nil {
		if r.Effective.From != nil {
			meta["effective_from"] = *r.Effective.From
		}
		if r.Effective.To != nil {
			meta["effective_to"] = *r.Effective.To
		}
	}
	items := []evidence.Item{{
		Kind: "RULE",
		Ref:  r.ID + "@" + r.Version,
		Meta: meta,
	}}
	// Each read field is its own citation, sorted by the load-time read list
	// so the evidence is byte-identical across identical inputs.
	for _, path := range r.reads {
		if v, err := valueFrom(fields, path, e.env[path]); err == nil {
			items = append(items, evidence.Item{
				Kind:    "FIELD",
				Ref:     path,
				Excerpt: valueExcerpt(v),
			})
		}
	}
	return items
}

func valueExcerpt(v value) string {
	switch v.kind {
	case KBool:
		if v.b {
			return "true"
		}
		return "false"
	case KNumber:
		return strconv.FormatFloat(v.n, 'f', -1, 64)
	case KString:
		return v.s
	case KDate:
		return time.Unix(v.d*86400, 0).UTC().Format("2006-01-02")
	}
	return ""
}

// typeEnv flattens the compiled subject schema into dotted path -> kind.
// JSON "integer" and "number" both become the rule language's number; a
// "date" is a schema string with format date, declared by the ruleset author
// as a date-typed field in the schema (type: string + a "format: date" key
// is outside this subset, so date fields are typed by convention: the path
// ends in _date or _at).
func typeEnv(s *schema.Schema) (TypeEnv, error) {
	fields := s.Fields()
	env := make(TypeEnv, len(fields))
	for path, typ := range fields {
		switch typ {
		case "string":
			if isDateField(path) {
				env[path] = KDate
			} else {
				env[path] = KString
			}
		case "number", "integer":
			env[path] = KNumber
		case "boolean":
			env[path] = KBool
		case "array":
			env[path] = KArray
		case "object":
			env[path] = KObject
		default:
			env[path] = KNull
		}
	}
	return env, nil
}

func isDateField(path string) bool {
	name := path
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	return strings.HasSuffix(name, "_date") || strings.HasSuffix(name, "_at") || name == "date"
}
