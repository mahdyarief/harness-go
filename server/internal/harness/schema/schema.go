// Package schema validates tool arguments against the JSON Schema subset the
// harness publishes to the model: object roots with typed properties, enums,
// numeric ranges, string lengths, arrays, and required keys.
package schema

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"sync"
)

// supported lists the F5 keyword subset. Anything else fails compilation so an
// unenforceable schema never reaches the model.
var supported = map[string]bool{
	"type": true, "enum": true, "const": true, "required": true,
	"properties": true, "additionalProperties": true, "items": true,
	"minItems": true, "maxItems": true, "minimum": true, "maximum": true,
	"minLength": true, "maxLength": true, "pattern": true,
}

type Schema struct {
	root *field
}

type field struct {
	types      []string
	enum       []any
	constVal   json.RawMessage
	minimum    *float64
	maximum    *float64
	minLen     *int
	maxLen     *int
	pattern    *regexp.Regexp
	minItems   *int
	maxItems   *int
	items      *field
	properties map[string]*field
	required   []string
	allowExtra bool
}

// Violation names the argument path and why it was rejected.
type Violation struct {
	Path   string
	Reason string
}

var errRoot = errors.New("schema: root must be an object")

// ErrUnsupported reports a keyword outside the enforced subset. Callers test it
// with errors.Is instead of matching the message.
var ErrUnsupported = errors.New("SCHEMA_UNSUPPORTED")

type compileResult struct {
	schema *Schema
	err    error
}

// Compiled schemas are immutable, so the same tool schema is compiled once and
// shared. Without this, every dispatched call recompiled patterns and types.
var compiledCache sync.Map

func Compile(raw json.RawMessage) (*Schema, error) {
	key := sha256.Sum256(raw)
	if cached, ok := compiledCache.Load(key); ok {
		result := cached.(compileResult)
		return result.schema, result.err
	}
	schema, err := compile(raw)
	compiledCache.Store(key, compileResult{schema: schema, err: err})
	return schema, err
}

func compile(raw json.RawMessage) (*Schema, error) {
	f, err := compileField(raw)
	if err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	if len(f.types) != 1 || f.types[0] != "object" {
		return nil, errRoot
	}
	return &Schema{root: f}, nil
}

func compileField(raw json.RawMessage) (*field, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, err
	}
	for key := range keys {
		if !supported[key] {
			return nil, fmt.Errorf("%w: keyword %q", ErrUnsupported, key)
		}
	}
	var spec struct {
		Type                 string                     `json:"type"`
		Enum                 []any                      `json:"enum"`
		Const                json.RawMessage            `json:"const"`
		Minimum              *float64                   `json:"minimum"`
		Maximum              *float64                   `json:"maximum"`
		MinLength            *int                       `json:"minLength"`
		MaxLength            *int                       `json:"maxLength"`
		Pattern              string                     `json:"pattern"`
		MinItems             *int                       `json:"minItems"`
		MaxItems             *int                       `json:"maxItems"`
		Items                json.RawMessage            `json:"items"`
		Properties           map[string]json.RawMessage `json:"properties"`
		Required             []string                   `json:"required"`
		AdditionalProperties *bool                      `json:"additionalProperties"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&spec); err != nil {
		return nil, err
	}
	f := &field{
		enum:       spec.Enum,
		constVal:   spec.Const,
		minimum:    spec.Minimum,
		maximum:    spec.Maximum,
		minLen:     spec.MinLength,
		maxLen:     spec.MaxLength,
		minItems:   spec.MinItems,
		maxItems:   spec.MaxItems,
		required:   spec.Required,
		allowExtra: true,
	}
	if spec.Pattern != "" {
		re, err := regexp.Compile(spec.Pattern)
		if err != nil {
			return nil, fmt.Errorf("pattern %q: %w", spec.Pattern, err)
		}
		f.pattern = re
	}
	if spec.AdditionalProperties != nil {
		f.allowExtra = *spec.AdditionalProperties
	}
	if spec.Type != "" {
		f.types = []string{spec.Type}
	}
	if len(spec.Items) > 0 {
		item, err := compileField(spec.Items)
		if err != nil {
			return nil, err
		}
		f.items = item
	}
	for name, child := range spec.Properties {
		cf, err := compileField(child)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", name, err)
		}
		if f.properties == nil {
			f.properties = map[string]*field{}
		}
		f.properties[name] = cf
	}
	return f, nil
}

func (s *Schema) Validate(raw json.RawMessage) []Violation {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var input map[string]any
	if err := dec.Decode(&input); err != nil || input == nil {
		return []Violation{{Path: ".", Reason: "argumen harus berupa objek JSON yang valid"}}
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return []Violation{{Path: ".", Reason: "argumen berisi data tambahan setelah objek JSON"}}
	}
	return s.root.validateObject("", input)
}

func (f *field) validate(path string, value any) []Violation {
	if len(f.types) > 0 && !matchesType(f.types, value) {
		return []Violation{{Path: path, Reason: fmt.Sprintf("tipe harus %v", f.types)}}
	}
	if len(f.enum) > 0 && !enumHas(f.enum, value) {
		return []Violation{{Path: path, Reason: fmt.Sprintf("harus salah satu dari %v", f.enum)}}
	}
	if len(f.constVal) > 0 && !constEquals(f.constVal, value) {
		return []Violation{{Path: path, Reason: "harus bernilai " + string(f.constVal)}}
	}
	switch v := value.(type) {
	case string:
		if f.minLen != nil && len([]rune(v)) < *f.minLen {
			return []Violation{{Path: path, Reason: fmt.Sprintf("panjang minimal %d", *f.minLen)}}
		}
		if f.maxLen != nil && len([]rune(v)) > *f.maxLen {
			return []Violation{{Path: path, Reason: fmt.Sprintf("panjang maksimal %d", *f.maxLen)}}
		}
		if f.pattern != nil && !f.pattern.MatchString(v) {
			return []Violation{{Path: path, Reason: "harus cocok pola " + f.pattern.String()}}
		}
	case bool, nil:
	case json.Number:
		n, err := v.Float64()
		if err != nil {
			return []Violation{{Path: path, Reason: "angka tidak valid"}}
		}
		if f.minimum != nil && n < *f.minimum {
			return []Violation{{Path: path, Reason: fmt.Sprintf("minimal %v", *f.minimum)}}
		}
		if f.maximum != nil && n > *f.maximum {
			return []Violation{{Path: path, Reason: fmt.Sprintf("maksimal %v", *f.maximum)}}
		}
	case []any:
		if f.minItems != nil && len(v) < *f.minItems {
			return []Violation{{Path: path, Reason: fmt.Sprintf("minimal %d item", *f.minItems)}}
		}
		if f.maxItems != nil && len(v) > *f.maxItems {
			return []Violation{{Path: path, Reason: fmt.Sprintf("maksimal %d item", *f.maxItems)}}
		}
		if f.items == nil {
			return nil
		}
		var out []Violation
		for i, item := range v {
			out = append(out, f.items.validate(fmt.Sprintf("%s[%d]", path, i), item)...)
		}
		return out
	case map[string]any:
		return f.validateObject(path, v)
	}
	return nil
}

func (f *field) validateObject(path string, value map[string]any) []Violation {
	var out []Violation
	for _, key := range f.required {
		if _, ok := value[key]; !ok {
			out = append(out, Violation{Path: join(path, key), Reason: "wajib diisi"})
		}
	}
	for key, item := range value {
		child, ok := f.properties[key]
		if !ok {
			if !f.allowExtra {
				out = append(out, Violation{Path: join(path, key), Reason: "tidak dideklarasikan pada skema"})
			}
			continue
		}
		out = append(out, child.validate(join(path, key), item)...)
	}
	return out
}

func join(path, key string) string { return path + "." + key }

func matchesType(types []string, value any) bool {
	for _, want := range types {
		switch want {
		case "string":
			if _, ok := value.(string); ok {
				return true
			}
		case "number":
			if _, ok := value.(json.Number); ok {
				return true
			}
		case "integer":
			if n, ok := value.(json.Number); ok {
				if f, err := n.Float64(); err == nil && f == math.Trunc(f) {
					return true
				}
			}
		case "boolean":
			if _, ok := value.(bool); ok {
				return true
			}
		case "array":
			if _, ok := value.([]any); ok {
				return true
			}
		case "object":
			if _, ok := value.(map[string]any); ok {
				return true
			}
		}
	}
	return false
}

func enumHas(list []any, value any) bool {
	for _, item := range list {
		if jsonEqual(item, value) {
			return true
		}
	}
	return false
}

func jsonEqual(a, b any) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		return sameNumber(x, y)
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for key, value := range x {
			other, ok := y[key]
			if !ok || !jsonEqual(value, other) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i, value := range x {
			if !jsonEqual(value, y[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.TypeOf(a) == reflect.TypeOf(b) && a == b
	}
}

func constEquals(raw json.RawMessage, value any) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var want any
	if err := dec.Decode(&want); err != nil {
		return false
	}
	return jsonEqual(want, value)
}

// sameNumber compares JSON numbers exactly; Float64 would make adjacent
// integers above 2^53 equal.
func sameNumber(a, b json.Number) bool {
	ra, okA := new(big.Rat).SetString(a.String())
	rb, okB := new(big.Rat).SetString(b.String())
	if okA && okB {
		return ra.Cmp(rb) == 0
	}
	return a.String() == b.String()
}
