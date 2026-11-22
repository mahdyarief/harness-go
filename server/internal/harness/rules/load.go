package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// LoadYAML parses a ruleset document (YAML or JSON; YAML is a JSON
// superset, so one decoder serves both) and returns the rules with their
// source line, so a load error can point at the offending rule.
func LoadYAML(r io.Reader) ([]Rule, error) {
	var doc yaml.Node
	if err := yaml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("ruleset: %w", err)
	}
	seq := rulesSequence(&doc)
	if seq == nil || len(seq.Content) == 0 {
		return nil, fmt.Errorf("ruleset: tidak ada aturan")
	}
	rules := make([]Rule, 0, len(seq.Content))
	for _, item := range seq.Content {
		var rule Rule
		if err := item.Decode(&rule); err != nil {
			return nil, fmt.Errorf("ruleset baris %d: %w", item.Line, err)
		}
		rule.line = item.Line
		rules = append(rules, rule)
	}
	return rules, nil
}

// rulesSequence finds the top-level "rules" sequence under the document
// mapping, tolerating a bare sequence document.
func rulesSequence(doc *yaml.Node) *yaml.Node {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind == yaml.SequenceNode {
		return root
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "rules" && root.Content[i+1].Kind == yaml.SequenceNode {
			return root.Content[i+1]
		}
	}
	return nil
}

// LoadJSON is LoadYAML for an already-encoded JSON document.
func LoadJSON(raw json.RawMessage) ([]Rule, error) {
	return LoadYAML(bytes.NewReader(raw))
}
