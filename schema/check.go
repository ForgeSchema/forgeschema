package schema

import (
	"fmt"
	"strings"
)

// Check is a named CHECK table constraint. Expression uses a small grammar
// that round-trips exactly through PostgreSQL's catalog form; see SCHEMA.md.
type Check struct {
	Name       string `yaml:"name" json:"name"`
	Expression string `yaml:"expression" json:"expression"`
}

const (
	maxCheckTerms  = 32
	maxCheckValues = 100
	maxCheckBytes  = 4096
)

// Check expressions are a conjunction of terms:
//
//	operand op literal | operand op column | operand IN (literal, ...)
//
// where operand is a column or char_length(column) and op is one of
// = <> < <= > >=. The canonical spelling puts single spaces around operators,
// joins terms with " AND " and writes a one-value IN list as =.

type token struct {
	kind string // ident, number, string, op, punct, word, end
	text string
}

func tokenize(s string) ([]token, error) {
	var out []token
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(' || c == ')' || c == ',' || c == '[' || c == ']':
			out = append(out, token{"punct", string(c)})
			i++
		case c == ':' && i+1 < len(s) && s[i+1] == ':':
			out = append(out, token{"punct", "::"})
			i += 2
		case c == '<' || c == '>' || c == '=' || c == '!':
			j := i + 1
			if j < len(s) && (s[j] == '=' || (c == '<' && s[j] == '>')) {
				j++
			}
			op := s[i:j]
			if op == "!" {
				return nil, fmt.Errorf("unexpected !")
			}
			if op == "!=" {
				op = "<>"
			}
			out = append(out, token{"op", op})
			i = j
		case c == '\'':
			j := i + 1
			for {
				if j >= len(s) {
					return nil, fmt.Errorf("unterminated string literal")
				}
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			lit := s[i : j+1]
			if !stringLiteral.MatchString(lit) {
				return nil, fmt.Errorf("string literals cannot contain backslashes or NUL")
			}
			out = append(out, token{"string", lit})
			i = j + 1
		case c == '"':
			j := strings.IndexByte(s[i+1:], '"')
			if j < 0 {
				return nil, fmt.Errorf("unterminated quoted identifier")
			}
			name := s[i+1 : i+1+j]
			if !validName(name) {
				return nil, fmt.Errorf("invalid column name %q", name)
			}
			out = append(out, token{"ident", name})
			i += j + 2
		case c == '-' || (c >= '0' && c <= '9'):
			j := i + 1
			for j < len(s) && ((s[j] >= '0' && s[j] <= '9') || s[j] == '.') {
				j++
			}
			out = append(out, token{"number", s[i:j]})
			i = j
		case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			j := i + 1
			for j < len(s) && (s[j] == '_' || (s[j] >= 'a' && s[j] <= 'z') || (s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			w := s[i:j]
			switch strings.ToLower(w) {
			case "and", "in", "any", "array":
				out = append(out, token{"word", strings.ToLower(w)})
			case "true", "false":
				out = append(out, token{"number", strings.ToLower(w)})
			default:
				out = append(out, token{"ident", w})
			}
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q", c)
		}
	}
	return append(out, token{"end", ""}), nil
}

// node is a parsed expression before it is checked against column types.
type node struct {
	kind  string // col, lit, cast, length, array, cmp, any, in, and
	text  string // column name, literal text, cast type or operator
	isStr bool
	args  []*node
}

type parser struct {
	toks    []token
	pos     int
	catalog bool
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) accept(kind, text string) bool {
	if t := p.peek(); t.kind == kind && t.text == text {
		p.pos++
		return true
	}
	return false
}
func (p *parser) expect(kind, text string) error {
	if !p.accept(kind, text) {
		return fmt.Errorf("expected %s", text)
	}
	return nil
}

func (p *parser) and() (*node, error) {
	first, err := p.comparison()
	if err != nil {
		return nil, err
	}
	n := &node{kind: "and", args: []*node{first}}
	for p.accept("word", "and") {
		next, err := p.comparison()
		if err != nil {
			return nil, err
		}
		n.args = append(n.args, next)
	}
	if len(n.args) == 1 {
		return first, nil
	}
	return n, nil
}

func (p *parser) comparison() (*node, error) {
	left, err := p.value()
	if err != nil {
		return nil, err
	}
	switch t := p.peek(); {
	case t.kind == "op":
		p.next()
		if p.catalog && t.text == "=" && p.accept("word", "any") {
			if err = p.expect("punct", "("); err != nil {
				return nil, err
			}
			arr, err := p.value()
			if err != nil {
				return nil, err
			}
			if err = p.expect("punct", ")"); err != nil {
				return nil, err
			}
			return &node{kind: "any", args: []*node{left, arr}}, nil
		}
		right, err := p.value()
		if err != nil {
			return nil, err
		}
		return &node{kind: "cmp", text: t.text, args: []*node{left, right}}, nil
	case t.kind == "word" && t.text == "in":
		p.next()
		if err = p.expect("punct", "("); err != nil {
			return nil, err
		}
		n := &node{kind: "in", args: []*node{left}}
		for {
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			n.args = append(n.args, v)
			if !p.accept("punct", ",") {
				break
			}
		}
		return n, p.expect("punct", ")")
	}
	return left, nil
}

// castType reads a type name after ::, such as text, character varying or text[].
func (p *parser) castType() (string, error) {
	t := p.next()
	if t.kind != "ident" {
		return "", fmt.Errorf("expected a type name")
	}
	name := t.text
	if (name == "character" || name == "double") && p.peek().kind == "ident" {
		name += " " + p.next().text
	}
	if p.accept("punct", "[") {
		if err := p.expect("punct", "]"); err != nil {
			return "", err
		}
		name += "[]"
	}
	return name, nil
}

func (p *parser) value() (*node, error) {
	var n *node
	switch t := p.next(); {
	case t.kind == "punct" && t.text == "(":
		inner, err := p.and()
		if err != nil {
			return nil, err
		}
		if err = p.expect("punct", ")"); err != nil {
			return nil, err
		}
		n = inner
	case t.kind == "word" && t.text == "array" && p.catalog:
		if err := p.expect("punct", "["); err != nil {
			return nil, err
		}
		n = &node{kind: "array"}
		for {
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			n.args = append(n.args, v)
			if !p.accept("punct", ",") {
				break
			}
		}
		if err := p.expect("punct", "]"); err != nil {
			return nil, err
		}
	case t.kind == "ident" && strings.ToLower(t.text) == "char_length" && p.peek().kind == "punct" && p.peek().text == "(":
		p.next()
		arg, err := p.value()
		if err != nil {
			return nil, err
		}
		if err = p.expect("punct", ")"); err != nil {
			return nil, err
		}
		n = &node{kind: "length", args: []*node{arg}}
	case t.kind == "ident":
		if !validName(t.text) {
			return nil, fmt.Errorf("invalid column name %q", t.text)
		}
		n = &node{kind: "col", text: t.text}
	case t.kind == "number":
		n = &node{kind: "lit", text: t.text}
	case t.kind == "string":
		n = &node{kind: "lit", text: t.text, isStr: true}
	default:
		if t.kind == "end" {
			return nil, fmt.Errorf("unexpected end of expression")
		}
		return nil, fmt.Errorf("unexpected %q", t.text)
	}
	for p.peek().kind == "punct" && p.peek().text == "::" {
		if !p.catalog {
			return nil, fmt.Errorf("casts are not part of the check grammar")
		}
		p.next()
		typ, err := p.castType()
		if err != nil {
			return nil, err
		}
		n = &node{kind: "cast", text: typ, args: []*node{n}}
	}
	return n, nil
}

func parseCheck(s string, catalog bool) (*node, error) {
	if len(s) > maxCheckBytes {
		return nil, fmt.Errorf("check expressions are limited to %d bytes", maxCheckBytes)
	}
	toks, err := tokenize(s)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, catalog: catalog}
	n, err := p.and()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != "end" {
		return nil, fmt.Errorf("unexpected %q", p.peek().text)
	}
	return n, nil
}

// term is one validated comparison.
type term struct {
	column string
	length bool     // char_length(column)
	op     string   // comparison operator; "in" for a list
	other  string   // right-hand column, if any
	values []string // canonical literals
}

func (t term) render(ident func(string) string) string {
	left := ident(t.column)
	if t.length {
		left = "char_length(" + left + ")"
	}
	switch {
	case t.other != "":
		return left + " " + t.op + " " + ident(t.other)
	case t.op == "in":
		return left + " IN (" + strings.Join(t.values, ", ") + ")"
	}
	return left + " " + t.op + " " + t.values[0]
}

// literalTypes are the column types a check may compare with literals.
func literalType(typ string) bool {
	return IntegerType(typ) || strings.HasPrefix(typ, "numeric") || TextType(typ) || typ == "boolean"
}

// columnTypes are the types a check may compare between two columns.
func columnType(typ string) bool {
	return literalType(typ) || typ == "date" || typ == "uuid" || typ == "real" || typ == "double precision" ||
		typ == "timestamp with time zone" || typ == "timestamp without time zone"
}

type checker struct {
	cols    map[string]Column
	catalog bool
}

// operand resolves a column or char_length(column), stripping the text cast
// PostgreSQL adds to a varchar column in catalog form.
func (c checker) operand(n *node) (name string, length bool, typ string, err error) {
	if n.kind == "length" {
		name, inner, typ, err := c.operand(n.args[0])
		if err != nil {
			return "", false, "", err
		}
		if inner || !TextType(typ) {
			return "", false, "", fmt.Errorf("char_length takes a text, varchar or char column")
		}
		return name, true, "integer", nil
	}
	if n.kind == "cast" && c.catalog && n.text == "text" && n.args[0].kind == "col" {
		col, ok := c.cols[n.args[0].text]
		if ok && strings.HasPrefix(col.Type, "character varying") {
			return col.Name, false, col.Type, nil
		}
	}
	if n.kind != "col" {
		return "", false, "", fmt.Errorf("the left side of a comparison must be a column or char_length(column)")
	}
	col, ok := c.cols[n.text]
	if !ok {
		return "", false, "", fmt.Errorf("unknown column %q", n.text)
	}
	return col.Name, false, col.Type, nil
}

// literal validates a literal against the type it is compared with, after
// stripping the value-preserving casts PostgreSQL adds in catalog form.
func (c checker) literal(n *node, typ string) (string, error) {
	if n.kind == "cast" && c.catalog {
		// PostgreSQL may wrap a literal in several casts, such as
		// ('a'::character varying)::text after a varchar column became text.
		var casts []string
		for n.kind == "cast" {
			casts = append(casts, n.text)
			n = n.args[0]
		}
		if n.kind != "lit" {
			return "", fmt.Errorf("unsupported cast")
		}
		v := n.text
		number := IntegerType(typ) || strings.HasPrefix(typ, "numeric")
		if number && n.isStr {
			v = v[1 : len(v)-1]
		}
		for _, cast := range casts {
			ok := false
			switch {
			case number:
				// Casting to an integer type rounds a decimal, so it only
				// keeps the value of an integer literal.
				ok = cast == "numeric" && decimalLiteral.MatchString(v) ||
					(cast == "integer" || cast == "bigint" || cast == "smallint") && integerLiteral.MatchString(v)
			case strings.HasPrefix(typ, "character("):
				ok = n.isStr && cast == "bpchar"
			case TextType(typ):
				ok = n.isStr && (cast == "text" || cast == "character varying")
			}
			if !ok {
				return "", fmt.Errorf("unsupported cast")
			}
		}
		return NormalizeDefault(typ, v)
	}
	if n.kind != "lit" {
		return "", fmt.Errorf("expected a literal")
	}
	if !IntegerType(typ) && !strings.HasPrefix(typ, "numeric") && n.isStr && !TextType(typ) {
		return "", fmt.Errorf("unexpected string literal")
	}
	if (IntegerType(typ) || strings.HasPrefix(typ, "numeric") || typ == "boolean") && n.isStr {
		return "", fmt.Errorf("compare %s with an unquoted literal", typ)
	}
	return NormalizeDefault(typ, n.text)
}

func (c checker) terms(n *node) ([]term, error) {
	if n.kind == "and" {
		var out []term
		for _, a := range n.args {
			ts, err := c.terms(a)
			if err != nil {
				return nil, err
			}
			out = append(out, ts...)
		}
		return out, nil
	}
	var t term
	var typ string
	var err error
	switch n.kind {
	case "cmp", "any", "in":
		if t.column, t.length, typ, err = c.operand(n.args[0]); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("each term must be a comparison")
	}
	var values []*node
	switch n.kind {
	case "cmp":
		t.op = n.text
		right := n.args[1]
		if right.kind == "col" || right.kind == "length" || (right.kind == "cast" && right.args[0].kind == "col") {
			name, length, rtyp, err := c.operand(right)
			if err != nil {
				return nil, err
			}
			if length || t.length {
				return nil, fmt.Errorf("char_length is compared with integer literals only")
			}
			if !columnType(typ) || !columnType(rtyp) || !compatible(typ, rtyp) {
				return nil, fmt.Errorf("columns %s (%s) and %s (%s) cannot be compared in a check", t.column, typ, name, rtyp)
			}
			t.other = name
			return []term{t}, nil
		}
		values = []*node{right}
	case "in":
		t.op = "in"
		values = n.args[1:]
	case "any":
		t.op = "in"
		arr := n.args[1]
		if arr.kind == "cast" && arr.text == "text[]" && strings.HasPrefix(typ, "character varying") {
			arr = arr.args[0]
		}
		if arr.kind != "array" {
			return nil, fmt.Errorf("unsupported ANY form")
		}
		values = arr.args
	}
	if !literalType(typ) {
		return nil, fmt.Errorf("checks compare literals with integer, numeric, text and boolean columns only; %s is %s", t.column, typ)
	}
	if len(values) > maxCheckValues {
		return nil, fmt.Errorf("IN lists are limited to %d values", maxCheckValues)
	}
	for _, v := range values {
		lit, err := c.literal(v, typ)
		if err != nil {
			return nil, err
		}
		t.values = append(t.values, lit)
	}
	if t.op == "in" && len(t.values) == 1 {
		t.op = "="
	}
	return []term{t}, nil
}

func checkTerms(cols map[string]Column, expr string, catalog bool) ([]term, error) {
	n, err := parseCheck(expr, catalog)
	if err != nil {
		return nil, err
	}
	ts, err := checker{cols, catalog}.terms(n)
	if err != nil {
		return nil, err
	}
	if len(ts) > maxCheckTerms {
		return nil, fmt.Errorf("check expressions are limited to %d terms", maxCheckTerms)
	}
	return ts, nil
}

func renderTerms(ts []term, ident func(string) string) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = t.render(ident)
	}
	return strings.Join(parts, " AND ")
}

// plainIdent leaves names bare unless they read as a word of the grammar.
func plainIdent(s string) string {
	switch s {
	case "and", "in", "any", "array", "true", "false":
		return quoteIdent(s)
	}
	return s
}
func quoteIdent(s string) string { return `"` + s + `"` }

// NormalizeCheck validates a check expression against a table's columns and
// returns its canonical spelling.
func NormalizeCheck(cols map[string]Column, expr string) (string, error) {
	ts, err := checkTerms(cols, expr, false)
	if err != nil {
		return "", err
	}
	return renderTerms(ts, plainIdent), nil
}

// CheckFromCatalog converts pg_get_constraintdef output (CHECK (...)) to the
// canonical expression, or reports false when it is outside the grammar.
func CheckFromCatalog(cols map[string]Column, def string) (string, bool) {
	if !strings.HasPrefix(def, "CHECK (") || !strings.HasSuffix(def, ")") {
		return "", false
	}
	ts, err := checkTerms(cols, def[len("CHECK ("):len(def)-1], true)
	if err != nil {
		return "", false
	}
	return renderTerms(ts, plainIdent), true
}

// CheckSQL renders a canonical check expression with quoted identifiers.
func CheckSQL(cols map[string]Column, expr string) string {
	ts, err := checkTerms(cols, expr, false)
	if err != nil {
		return ""
	}
	return renderTerms(ts, quoteIdent)
}

// RenameCheckColumns rewrites the column names in a canonical check
// expression over cols, as PostgreSQL does when a column is renamed.
func RenameCheckColumns(cols map[string]Column, expr string, renames map[string]string) string {
	if len(renames) == 0 {
		return expr
	}
	ts, err := checkTerms(cols, expr, false)
	if err != nil {
		return expr
	}
	for i := range ts {
		if next, ok := renames[ts[i].column]; ok {
			ts[i].column = next
		}
		if next, ok := renames[ts[i].other]; ok && ts[i].other != "" {
			ts[i].other = next
		}
	}
	return renderTerms(ts, plainIdent)
}

// CheckColumns lists the columns a canonical check expression over cols uses.
func CheckColumns(cols map[string]Column, expr string) []string {
	ts, err := checkTerms(cols, expr, false)
	if err != nil {
		return nil
	}
	var out []string
	for _, t := range ts {
		out = append(out, t.column)
		if t.other != "" {
			out = append(out, t.other)
		}
	}
	return out
}
