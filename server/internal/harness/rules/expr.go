// Package rules implements a declarative, versioned rule engine. Rules are
// data (YAML/JSON), expressions are pure (no I/O, no loops), and an unknown
// field is rejected at load time against the subject's JSON schema.
package rules

import (
	"fmt"
	"strings"
	"time"
)

// Kind is the static type of an expression node.
type Kind int

const (
	KInvalid Kind = iota
	KBool
	KNumber
	KString
	KDate
	KArray
	KObject
	KNull
)

func (k Kind) String() string {
	switch k {
	case KBool:
		return "bool"
	case KNumber:
		return "number"
	case KString:
		return "string"
	case KDate:
		return "date"
	case KArray:
		return "array"
	case KObject:
		return "object"
	case KNull:
		return "null"
	default:
		return "invalid"
	}
}

// value is a runtime value. Dates are days since the Unix epoch, so a
// date - date subtraction is directly a day count.
type value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	d    int64
	arr  []value
}

func numberValue(n float64) value { return value{kind: KNumber, n: n} }
func stringValue(s string) value  { return value{kind: KString, s: s} }
func dateValue(d int64) value     { return value{kind: KDate, d: d} }
func boolValue(b bool) value      { return value{kind: KBool, b: b} }
func arrayValue(a []value) value  { return value{kind: KArray, arr: a} }
func nullValue() value            { return value{kind: KNull} }

func parseDate(raw string) (int64, error) {
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return 0, fmt.Errorf("tanggal %q tidak valid: %w", raw, err)
	}
	return t.Unix() / 86400, nil
}

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tString
	tNumber
	tOp
)

type token struct {
	kind tokKind
	text string
	pos  int
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}
func isIdent(c byte) bool { return isAlpha(c) || isDigit(c) }

func lex(src string) ([]token, error) {
	var out []token
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '"':
			j := i + 1
			var b strings.Builder
			for j < len(src) && src[j] != '"' {
				if src[j] == '\\' && j+1 < len(src) {
					j++
				}
				b.WriteByte(src[j])
				j++
			}
			if j >= len(src) {
				return nil, fmt.Errorf("string tidak ditutup di posisi %d", i)
			}
			out = append(out, token{tString, b.String(), i})
			i = j + 1
		case isDigit(c):
			j := i + 1
			for j < len(src) && (isDigit(src[j]) || src[j] == '.') {
				j++
			}
			out = append(out, token{tNumber, src[i:j], i})
			i = j
		case isAlpha(c):
			j := i + 1
			for j < len(src) && (isIdent(src[j]) || src[j] == '.') {
				if src[j] == '.' && (j+1 >= len(src) || !isAlpha(src[j+1])) {
					break
				}
				j++
			}
			out = append(out, token{tIdent, src[i:j], i})
			i = j
		case strings.ContainsRune("=!<>", rune(c)):
			op := string(c)
			if i+1 < len(src) && src[i+1] == '=' {
				op += "="
				i++
			}
			out = append(out, token{tOp, op, i})
			i++
		case c == '(' || c == ')' || c == '[' || c == ']' || c == ',' || c == '+' || c == '-':
			out = append(out, token{tOp, string(c), i})
			i++
		default:
			return nil, fmt.Errorf("karakter tak dikenal %q di posisi %d", c, i)
		}
	}
	return append(out, token{tEOF, "", len(src)}), nil
}
