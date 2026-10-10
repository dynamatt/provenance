package query

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Calculated-field formulas (DES-0012) are Excel-style:
//
//	arithmetic   + - * /         comparison   = <> < > <= >=
//	literals     2.5  "text"  TRUE  FALSE
//	functions    IF AND OR NOT ISBLANK  MAX MIN SUM COUNT AVG
//	references   field   link.field   list[].subfield   list[].link.field
//
// Function names and TRUE/FALSE are case-insensitive, as in Excel.

// Expr is a parsed formula expression.
type Expr interface{ pos() int }

type (
	// NumberLit, StringLit and BoolLit are literals.
	NumberLit struct {
		Pos   int
		Value float64
	}
	StringLit struct {
		Pos   int
		Value string
	}
	BoolLit struct {
		Pos   int
		Value bool
	}
	// RefExpr reads a field along a path of steps.
	RefExpr struct {
		Pos   int
		Steps []Step
	}
	// Binary is an arithmetic or comparison operation.
	Binary struct {
		Pos  int
		Op   string
		L, R Expr
	}
	// Negate is unary minus.
	Negate struct {
		Pos int
		X   Expr
	}
	// Call is a function call; Func is upper case.
	Call struct {
		Pos  int
		Func string
		Args []Expr
	}
)

// Step is one name in a reference; Each is set when it is followed by [].
type Step struct {
	Name string
	Each bool
}

func (e *NumberLit) pos() int { return e.Pos }
func (e *StringLit) pos() int { return e.Pos }
func (e *BoolLit) pos() int   { return e.Pos }
func (e *RefExpr) pos() int   { return e.Pos }
func (e *Binary) pos() int    { return e.Pos }
func (e *Negate) pos() int    { return e.Pos }
func (e *Call) pos() int      { return e.Pos }

func (r *RefExpr) String() string {
	var b strings.Builder
	for i, s := range r.Steps {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(s.Name)
		if s.Each {
			b.WriteString("[]")
		}
	}
	return b.String()
}

// FormulaError is a formula that cannot be parsed or compiled. Pos is the
// 1-based column in the formula.
type FormulaError struct {
	Pos int
	Msg string
}

func (e *FormulaError) Error() string {
	if e.Pos > 0 {
		return fmt.Sprintf("column %d: %s", e.Pos, e.Msg)
	}
	return e.Msg
}

type token struct {
	kind string // num, str, name, op, "(", ")", ",", "[]", ".", eof
	text string
	pos  int
}

func lex(src string) ([]token, error) {
	var out []token
	rs := []rune(src)
	for i := 0; i < len(rs); {
		r := rs[i]
		start := i + 1
		switch {
		case unicode.IsSpace(r):
			i++
		case unicode.IsDigit(r) || (r == '.' && i+1 < len(rs) && unicode.IsDigit(rs[i+1])):
			j := i
			for j < len(rs) && (unicode.IsDigit(rs[j]) || rs[j] == '.') {
				j++
			}
			out = append(out, token{"num", string(rs[i:j]), start})
			i = j
		case r == '"':
			// Excel strings: "" inside a string is one quote.
			var b strings.Builder
			j := i + 1
			for {
				if j >= len(rs) {
					return nil, &FormulaError{start, "unterminated string"}
				}
				if rs[j] == '"' {
					if j+1 < len(rs) && rs[j+1] == '"' {
						b.WriteRune('"')
						j += 2
						continue
					}
					break
				}
				b.WriteRune(rs[j])
				j++
			}
			out = append(out, token{"str", b.String(), start})
			i = j + 1
		case unicode.IsLetter(r) || r == '_':
			j := i
			for j < len(rs) && (unicode.IsLetter(rs[j]) || unicode.IsDigit(rs[j]) || rs[j] == '_') {
				j++
			}
			out = append(out, token{"name", string(rs[i:j]), start})
			i = j
		case r == '[':
			if i+1 >= len(rs) || rs[i+1] != ']' {
				return nil, &FormulaError{start, "expected [] (a list's rows)"}
			}
			out = append(out, token{"[]", "[]", start})
			i += 2
		case strings.ContainsRune("(),.", r):
			out = append(out, token{string(r), string(r), start})
			i++
		case strings.ContainsRune("+-*/=<>", r):
			op := string(r)
			if i+1 < len(rs) {
				if two := string(rs[i : i+2]); two == "<>" || two == "<=" || two == ">=" {
					op = two
				}
			}
			out = append(out, token{"op", op, start})
			i += len(op)
		default:
			return nil, &FormulaError{start, fmt.Sprintf("unexpected %q", r)}
		}
	}
	return append(out, token{"eof", "", len(rs) + 1}), nil
}

// ParseFormula parses an Excel-style formula. A leading "=" is allowed, as
// typed into a spreadsheet cell.
func ParseFormula(src string) (Expr, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	if p.peek().kind == "op" && p.peek().text == "=" {
		p.next()
	}
	e, err := p.comparison()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != "eof" {
		return nil, &FormulaError{t.pos, fmt.Sprintf("unexpected %q", t.text)}
	}
	return e, nil
}

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}

func (p *parser) isOp(ops ...string) bool {
	t := p.peek()
	return t.kind == "op" && slices.Contains(ops, t.text)
}

// The binary operators by precedence, loosest first: each level is its
// operands, from the next level, joined left to right.
func (p *parser) comparison() (Expr, error) {
	return p.binary(p.additive, "=", "<>", "<", ">", "<=", ">=")
}
func (p *parser) additive() (Expr, error)       { return p.binary(p.multiplicative, "+", "-") }
func (p *parser) multiplicative() (Expr, error) { return p.binary(p.unary, "*", "/") }

// binary parses operands joined by any of ops, left-associatively.
func (p *parser) binary(operand func() (Expr, error), ops ...string) (Expr, error) {
	l, err := operand()
	if err != nil {
		return nil, err
	}
	for p.isOp(ops...) {
		t := p.next()
		r, err := operand()
		if err != nil {
			return nil, err
		}
		l = &Binary{Pos: t.pos, Op: t.text, L: l, R: r}
	}
	return l, nil
}

func (p *parser) unary() (Expr, error) {
	if p.isOp("-", "+") {
		t := p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		if t.text == "+" {
			return x, nil
		}
		return &Negate{Pos: t.pos, X: x}, nil
	}
	return p.primary()
}

// Functions maps each function to its argument count: n ≥ 0 exactly n,
// -1 one or more.
var Functions = map[string]int{
	"IF": -1, "AND": -1, "OR": -1, "NOT": 1, "ISBLANK": 1,
	"MAX": -1, "MIN": -1, "SUM": -1, "COUNT": -1, "AVG": -1,
}

func functionNames() string {
	return "IF, AND, OR, NOT, ISBLANK, MAX, MIN, SUM, COUNT, AVG"
}

func (p *parser) primary() (Expr, error) {
	t := p.next()
	switch t.kind {
	case "num":
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, &FormulaError{t.pos, fmt.Sprintf("%q is not a number", t.text)}
		}
		return &NumberLit{Pos: t.pos, Value: f}, nil
	case "str":
		return &StringLit{Pos: t.pos, Value: t.text}, nil
	case "(":
		e, err := p.comparison()
		if err != nil {
			return nil, err
		}
		if c := p.next(); c.kind != ")" {
			return nil, &FormulaError{c.pos, "expected )"}
		}
		return e, nil
	case "name":
		upper := strings.ToUpper(t.text)
		if p.peek().kind == "(" {
			arity, ok := Functions[upper]
			if !ok {
				return nil, &FormulaError{t.pos, fmt.Sprintf("unknown function %s (functions: %s)", t.text, functionNames())}
			}
			p.next()
			var args []Expr
			if p.peek().kind != ")" {
				for {
					a, err := p.comparison()
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					if p.peek().kind != "," {
						break
					}
					p.next()
				}
			}
			if c := p.next(); c.kind != ")" {
				return nil, &FormulaError{c.pos, "expected , or )"}
			}
			switch {
			case arity >= 0 && len(args) != arity:
				return nil, &FormulaError{t.pos, fmt.Sprintf("%s takes %d argument(s), got %d", upper, arity, len(args))}
			case arity < 0 && len(args) == 0:
				return nil, &FormulaError{t.pos, fmt.Sprintf("%s needs at least one argument", upper)}
			case upper == "IF" && len(args) != 2 && len(args) != 3:
				return nil, &FormulaError{t.pos, fmt.Sprintf("IF takes 2 or 3 arguments, got %d", len(args))}
			}
			return &Call{Pos: t.pos, Func: upper, Args: args}, nil
		}
		switch upper {
		case "TRUE", "FALSE":
			return &BoolLit{Pos: t.pos, Value: upper == "TRUE"}, nil
		}
		ref := &RefExpr{Pos: t.pos, Steps: []Step{{Name: t.text}}}
		for {
			if p.peek().kind == "[]" {
				p.next()
				ref.Steps[len(ref.Steps)-1].Each = true
			}
			if p.peek().kind != "." {
				break
			}
			p.next()
			n := p.next()
			if n.kind != "name" {
				return nil, &FormulaError{n.pos, "expected a field name after ."}
			}
			ref.Steps = append(ref.Steps, Step{Name: n.text})
		}
		return ref, nil
	case "eof":
		return nil, &FormulaError{t.pos, "unexpected end of formula"}
	}
	return nil, &FormulaError{t.pos, fmt.Sprintf("unexpected %q", t.text)}
}
