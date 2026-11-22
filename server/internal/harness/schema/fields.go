package schema

import "sort"

// Fields returns every declared property as a dotted path mapped to its JSON
// type (the first type of a union). Nested objects are walked; array item
// fields are not, since the rule language cannot index into an array. The
// result backs static name-and-type checking for declarative rules.
func (s *Schema) Fields() map[string]string {
	out := map[string]string{}
	if s == nil || s.root == nil {
		return out
	}
	s.root.walkFields("", out)
	return out
}

func (f *field) walkFields(prefix string, out map[string]string) {
	for name, child := range f.properties {
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		typ := "null"
		if len(child.types) > 0 {
			typ = child.types[0]
		}
		out[path] = typ
		if len(child.properties) > 0 {
			child.walkFields(path, out)
		}
	}
}

// FieldPaths returns Fields' keys sorted, for deterministic listings.
func (s *Schema) FieldPaths() []string {
	fields := s.Fields()
	out := make([]string, 0, len(fields))
	for path := range fields {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
