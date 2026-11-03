package plan

import (
	"regexp"
	"strings"
)

// literalOrIdentifier matches, left to right, every quoted identifier and
// every string literal with the cast that may follow it, so a quote inside
// an identifier is never taken for the start of a literal.
var literalOrIdentifier = regexp.MustCompile(`"(?:[^"]|"")*"|'((?:[^']|'')*)'(?:::((?:"(?:[^"]|"")*"|[a-z_][a-z0-9_$]*)(?:\.(?:"(?:[^"]|"")*"|[a-z_][a-z0-9_$]*))?)(\[\])?)?`)

type enumCast struct {
	literal, enum string
	array         bool
}

// enumCasts lists the casts of literals to a type of the managed schema in a
// normalized view query: the literal's value, the type's bare name and
// whether it is an array cast.
func enumCasts(query, schemaName string) []enumCast {
	var out []enumCast
	for _, m := range literalOrIdentifier.FindAllStringSubmatch(query, -1) {
		if m[2] == "" {
			continue
		}
		typ := m[2]
		if i := lastDotOutsideQuotes(typ); i >= 0 {
			if unquote(typ[:i]) != schemaName {
				continue
			}
			typ = typ[i+1:]
		}
		out = append(out, enumCast{literal: strings.ReplaceAll(m[1], "''", "'"), enum: unquote(typ), array: m[3] != ""})
	}
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	}
	return s
}

func lastDotOutsideQuotes(s string) int {
	in, last := false, -1
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '"':
			in = !in
		case s[i] == '.' && !in:
			last = i
		}
	}
	return last
}

// arrayMentions reports whether an array literal such as {a,"b c"} may hold
// value; it errs on the side of yes.
func arrayMentions(literal, value string) bool {
	return strings.Contains(literal, value)
}
