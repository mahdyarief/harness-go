package rules

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// node is one parsed expression. Its type is computed once at parse time
// against the subject schema, so Evaluate never guesses types.
type node interface {
	kind() Kind
	eval(subject map[string]any, at timeValue) (value, error)
	fields(map[string]bool)
}

// timeValue carries the evaluation instant for "today".
type timeValue struct{ day int64 }

// TypeEnv maps dotted field paths to their static type. A path absent from
// the environment is unknown and rejected at load time.
type TypeEnv map[string]Kind

// Parse checks the expression syntax and statically types it against env.
func Parse(src string, env TypeEnv) (node, []string, error) {
	tokens, err := lex(src)
	if err != nil {
		return nil, nil, err
	}
	p := &parser{tokens: tokens, env: env, subject: map[string]Kind{}}
	n, err := p.parseOr()
	if err != nil {
		return nil, nil, err
	}
	if p.peek().kind != tEOF {
		return nil, nil, p.errorf("token tak terduga %q", p.peek().text)
	}
	return n, p.usedFields(), nil
}

type parser struct {
	tokens  []token
	pos     int
	env     TypeEnv
	subject map[string]Kind
}

func (p *parser) peek() token { return p.tokens[p.pos] }
func (p *parser) next() token {
	t := p.tokens[p.pos]
	if t.kind != tEOF {
		p.pos++
	}
	return t
}
func (p *parser) accept(text string) bool {
	if p.peek().text == text {
		p.next()
		return true
	}
	return false
}
func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("ekspresi posisi %d: %s", p.peek().pos, fmt.Sprintf(format, args...))
}

// Precedence (low to high): or, and, not, comparisons, +-, atoms.
func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.accept("or") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		if left.kind() != KBool || right.kind() != KBool {
			return nil, p.errorf("or memerlukan dua operand bool")
		}
		left = &binaryNode{op: "or", typ: KBool, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (node, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.accept("and") {
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		if left.kind() != KBool || right.kind() != KBool {
			return nil, p.errorf("and memerlukan dua operand bool")
		}
		left = &binaryNode{op: "and", typ: KBool, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseNot() (node, error) {
	if p.accept("not") {
		inner, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		if inner.kind() != KBool {
			return nil, p.errorf("not memerlukan operand bool")
		}
		return &unaryNode{op: "not", typ: KBool, inner: inner}, nil
	}
	return p.parseCompare()
}

func (p *parser) parseCompare() (node, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for {
		op := p.peek().text
		switch op {
		case "==", "!=", "<", "<=", ">", ">=", "in":
			p.next()
		default:
			return left, nil
		}
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		if err = checkComparison(op, left.kind(), right.kind()); err != nil {
			return nil, p.errorf("%s", err)
		}
		left = &binaryNode{op: op, typ: KBool, left: left, right: right}
	}
}

func checkComparison(op string, l, r Kind) error {
	switch op {
	case "in":
		if r != KArray && r != KString {
			return fmt.Errorf("in memerlukan sisi kanan array atau string, mendapat %s", r)
		}
		return nil
	case "==", "!=":
		if l == KInvalid || r == KInvalid || l != r && l != KNull && r != KNull {
			return fmt.Errorf("perbandingan %s antara %s dan %s tidak didukung", op, l, r)
		}
		return nil
	default:
		if l != r || l != KNumber && l != KDate && l != KString {
			return fmt.Errorf("perbandingan %s memerlukan dua operand bertipe sama (number/date/string)", op)
		}
		return nil
	}
}

func (p *parser) parseAdd() (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().text == "+" || p.peek().text == "-" {
		op := p.next().text
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		var typ Kind
		switch {
		case op == "-" && left.kind() == KDate && right.kind() == KDate:
			typ = KNumber // date - date = days
		case left.kind() == KDate && right.kind() == KNumber:
			typ = KDate // date +/- days
		case left.kind() == KNumber && right.kind() == KNumber:
			typ = KNumber
		case op == "+" && left.kind() == KString && right.kind() == KString:
			typ = KString
		default:
			return nil, p.errorf("operasi %s tidak didukung untuk %s dan %s", op, left.kind(), right.kind())
		}
		left = &binaryNode{op: op, typ: typ, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseUnary() (node, error) {
	if p.peek().text == "-" {
		p.next()
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		if inner.kind() != KNumber {
			return nil, p.errorf("minus memerlukan operand number")
		}
		return &unaryNode{op: "-", typ: KNumber, inner: inner}, nil
	}
	if p.peek().text == "[" {
		p.next()
		var items []node
		for p.peek().text != "]" {
			if len(items) > 0 && !p.accept(",") {
				return nil, p.errorf("koma antar elemen array hilang")
			}
			item, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		if !p.accept("]") {
			return nil, p.errorf("kurung siku tutup hilang")
		}
		return &arrayNode{items: items}, nil
	}
	return p.parseAtom()
}

func (p *parser) parseAtom() (node, error) {
	t := p.next()
	switch t.kind {
	case tNumber:
		n, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, p.errorf("angka tidak valid %q", t.text)
		}
		return &literalNode{v: numberValue(n)}, nil
	case tString:
		return &literalNode{v: stringValue(t.text)}, nil
	case tIdent:
		return p.parseIdent(t)
	case tOp:
		if t.text != "(" {
			return nil, p.errorf("token tak terduga %q", t.text)
		}
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.accept(")") {
			return nil, p.errorf("kurung tutup hilang")
		}
		return n, nil
	default:
		return nil, p.errorf("token tak terduga %q", t.text)
	}
}

func (p *parser) parseIdent(t token) (node, error) {
	switch t.text {
	case "true":
		return &literalNode{v: boolValue(true)}, nil
	case "false":
		return &literalNode{v: boolValue(false)}, nil
	case "date":
		if !p.accept("(") {
			return nil, p.errorf("date memerlukan tanda kurung")
		}
		s, err := p.parseOr()
		if err != nil || s.kind() != KString {
			return nil, p.errorf("date memerlukan argumen string")
		}
		if !p.accept(")") {
			return nil, p.errorf("kurung tutup hilang")
		}
		return &dateNode{arg: s}, nil
	case "len":
		if !p.accept("(") {
			return nil, p.errorf("len memerlukan tanda kurung")
		}
		arg, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if arg.kind() != KArray && arg.kind() != KString {
			return nil, p.errorf("len memerlukan array atau string")
		}
		if !p.accept(")") {
			return nil, p.errorf("kurung tutup hilang")
		}
		return &lenNode{arg: arg}, nil
	case "exists":
		if !p.accept("(") {
			return nil, p.errorf("exists memerlukan tanda kurung")
		}
		name := p.next()
		if name.kind != tIdent || !p.accept(")") {
			return nil, p.errorf("exists memerlukan nama field")
		}
		kind, ok := p.env[name.text]
		if !ok {
			return nil, fmt.Errorf("field %q tidak dikenal dalam skema subjek", name.text)
		}
		p.subject[name.text] = kind
		return &existsNode{name: name.text, typ: kind}, nil
	case "between":
		if !p.accept("(") {
			return nil, p.errorf("between memerlukan tanda kurung")
		}
		x, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.accept(",") {
			return nil, p.errorf("between memerlukan koma")
		}
		lo, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.accept(",") {
			return nil, p.errorf("between memerlukan koma kedua")
		}
		hi, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.accept(")") {
			return nil, p.errorf("kurung tutup hilang")
		}
		if x.kind() != lo.kind() || x.kind() != hi.kind() {
			return nil, p.errorf("between memerlukan tiga operand bertipe sama")
		}
		return &betweenNode{x: x, lo: lo, hi: hi}, nil
	}

	// A dotted field path. The lexer keeps "a.b.c" as one identifier token,
	// so no extra work is needed here.
	path := t.text
	kind, ok := p.env[path]
	if !ok {
		return nil, fmt.Errorf("field %q tidak dikenal dalam skema subjek", path)
	}
	p.subject[path] = kind
	return &fieldNode{name: path, typ: kind}, nil
}

// UsedFields returns the field paths this parsed expression reads in a
// deterministic order (sorted), so evidence is byte-identical across runs.
func (p *parser) usedFields() []string {
	out := make([]string, 0, len(p.subject))
	for name := range p.subject {
		out = append(out, name)
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

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
