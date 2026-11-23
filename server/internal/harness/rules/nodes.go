package rules

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- node implementations ---

type literalNode struct{ v value }

func (n *literalNode) kind() Kind { return n.v.kind }
func (n *literalNode) eval(map[string]any, timeValue) (value, error) {
	return n.v, nil
}
func (n *literalNode) fields(map[string]bool) {}

type fieldNode struct {
	name string
	typ  Kind
}

func (n *fieldNode) kind() Kind { return n.typ }
func (n *fieldNode) fields(m map[string]bool) {
	m[n.name] = true
}
func (n *fieldNode) eval(subject map[string]any, _ timeValue) (value, error) {
	return valueFrom(subject, n.name, n.typ)
}

type unaryNode struct {
	op    string
	typ   Kind
	inner node
}

func (n *unaryNode) kind() Kind { return n.typ }
func (n *unaryNode) fields(m map[string]bool) {
	n.inner.fields(m)
}
func (n *unaryNode) eval(s map[string]any, at timeValue) (value, error) {
	v, err := n.inner.eval(s, at)
	if err != nil {
		return value{}, err
	}
	if n.op == "not" {
		return boolValue(!v.b), nil
	}
	return numberValue(-v.n), nil
}

type binaryNode struct {
	op          string
	typ         Kind
	left, right node
}

func (n *binaryNode) kind() Kind { return n.typ }
func (n *binaryNode) fields(m map[string]bool) {
	n.left.fields(m)
	n.right.fields(m)
}
func (n *binaryNode) eval(s map[string]any, at timeValue) (value, error) {
	l, err := n.left.eval(s, at)
	if err != nil {
		return value{}, err
	}
	r, err := n.right.eval(s, at)
	if err != nil {
		return value{}, err
	}
	switch n.op {
	case "and":
		return boolValue(l.b && r.b), nil
	case "or":
		return boolValue(l.b || r.b), nil
	case "==":
		return boolValue(equal(l, r)), nil
	case "!=":
		return boolValue(!equal(l, r)), nil
	case "in":
		return boolValue(contains(r, l)), nil
	case "<", "<=", ">", ">=":
		return boolValue(compare(n.op, l, r)), nil
	case "+":
		switch n.typ {
		case KNumber:
			return numberValue(l.n + r.n), nil
		case KString:
			return stringValue(l.s + r.s), nil
		case KDate:
			return dateValue(l.d + int64(r.n)), nil
		}
	case "-":
		switch n.typ {
		case KNumber:
			if l.kind == KDate {
				return numberValue(float64(l.d - r.d)), nil
			}
			return numberValue(l.n - r.n), nil
		case KDate:
			return dateValue(l.d - int64(r.n)), nil
		}
	}
	return value{}, fmt.Errorf("operator %q tidak dapat dievaluasi", n.op)
}

type dateNode struct{ arg node }

func (n *dateNode) kind() Kind { return KDate }
func (n *dateNode) fields(m map[string]bool) {
	n.arg.fields(m)
}
func (n *dateNode) eval(s map[string]any, _ timeValue) (value, error) {
	v, err := n.arg.eval(s, timeValue{})
	if err != nil {
		return value{}, err
	}
	d, err := parseDate(v.s)
	return dateValue(d), err
}

type lenNode struct{ arg node }

func (n *lenNode) kind() Kind { return KNumber }
func (n *lenNode) fields(m map[string]bool) {
	n.arg.fields(m)
}
func (n *lenNode) eval(s map[string]any, at timeValue) (value, error) {
	v, err := n.arg.eval(s, at)
	if err != nil {
		return value{}, err
	}
	if v.kind == KString {
		return numberValue(float64(len([]rune(v.s)))), nil
	}
	return numberValue(float64(len(v.arr))), nil
}

type existsNode struct {
	name string
	typ  Kind
}

func (n *existsNode) kind() Kind { return KBool }
func (n *existsNode) fields(m map[string]bool) {
	m[n.name] = true
}
func (n *existsNode) eval(s map[string]any, _ timeValue) (value, error) {
	_, err := valueFrom(s, n.name, n.typ)
	return boolValue(err == nil), nil
}

type arrayNode struct{ items []node }

func (n *arrayNode) kind() Kind { return KArray }
func (n *arrayNode) fields(m map[string]bool) {
	for _, item := range n.items {
		item.fields(m)
	}
}
func (n *arrayNode) eval(s map[string]any, at timeValue) (value, error) {
	out := make([]value, len(n.items))
	for i, item := range n.items {
		v, err := item.eval(s, at)
		if err != nil {
			return value{}, err
		}
		out[i] = v
	}
	return arrayValue(out), nil
}

type betweenNode struct{ x, lo, hi node }

func (n *betweenNode) kind() Kind { return KBool }
func (n *betweenNode) fields(m map[string]bool) {
	n.x.fields(m)
	n.lo.fields(m)
	n.hi.fields(m)
}
func (n *betweenNode) eval(s map[string]any, at timeValue) (value, error) {
	x, err := n.x.eval(s, at)
	if err != nil {
		return value{}, err
	}
	lo, err := n.lo.eval(s, at)
	if err != nil {
		return value{}, err
	}
	hi, err := n.hi.eval(s, at)
	if err != nil {
		return value{}, err
	}
	return boolValue(compare(">=", x, lo) && compare("<=", x, hi)), nil
}

func equal(a, b value) bool {
	if a.kind == KNull || b.kind == KNull {
		return a.kind == b.kind
	}
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case KBool:
		return a.b == b.b
	case KNumber:
		return a.n == b.n
	case KString:
		return a.s == b.s
	case KDate:
		return a.d == b.d
	case KNull:
		return true
	}
	return false
}

func contains(container, item value) bool {
	if container.kind == KString {
		return strings.Contains(container.s, item.s)
	}
	for _, v := range container.arr {
		if equal(v, item) {
			return true
		}
	}
	return false
}

func compare(op string, a, b value) bool {
	switch a.kind {
	case KNumber:
		return cmpFloat(op, a.n, b.n)
	case KDate:
		return cmpFloat(op, float64(a.d), float64(b.d))
	case KString:
		c := strings.Compare(a.s, b.s)
		switch op {
		case "<":
			return c < 0
		case "<=":
			return c <= 0
		case ">":
			return c > 0
		default:
			return c >= 0
		}
	}
	return false
}

func cmpFloat(op string, a, b float64) bool {
	switch op {
	case "<":
		return a < b
	case "<=":
		return a <= b
	case ">":
		return a > b
	default:
		return a >= b
	}
}

// valueFrom reads a dotted path from a decoded subject, coercing JSON
// numbers and ISO date strings to their static types.
func valueFrom(subject map[string]any, path string, typ Kind) (value, error) {
	parts := strings.Split(path, ".")
	var cur any = subject
	for _, part := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			return value{}, fmt.Errorf("field %q bukan objek", path)
		}
		cur, ok = obj[part]
		if !ok {
			return value{}, fmt.Errorf("field %q tidak ada", path)
		}
	}
	switch typ {
	case KBool:
		b, ok := cur.(bool)
		if !ok {
			return value{}, fmt.Errorf("field %q bukan bool", path)
		}
		return boolValue(b), nil
	case KNumber:
		n, ok := cur.(json.Number)
		if !ok {
			return value{}, fmt.Errorf("field %q bukan number", path)
		}
		f, err := n.Float64()
		return numberValue(f), err
	case KString, KDate:
		s, ok := cur.(string)
		if !ok {
			return value{}, fmt.Errorf("field %q bukan string", path)
		}
		if typ == KDate {
			d, err := parseDate(s)
			return dateValue(d), err
		}
		return stringValue(s), nil
	case KArray:
		a, ok := cur.([]any)
		if !ok {
			return value{}, fmt.Errorf("field %q bukan array", path)
		}
		out := make([]value, len(a))
		for i, item := range a {
			switch v := item.(type) {
			case json.Number:
				n, _ := v.Float64()
				out[i] = numberValue(n)
			case string:
				out[i] = stringValue(v)
			case bool:
				out[i] = boolValue(v)
			default:
				out[i] = nullValue()
			}
		}
		return arrayValue(out), nil
	}
	return nullValue(), nil
}
