package rules

import (
	"fmt"
	"strconv"
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
