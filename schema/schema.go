// Package schema reads and validates ForgeSchema's versioned desired schema.
package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ForgeSchema/forgeschema/internal/strictyaml"
)

const MaxBytes = strictyaml.MaxBytes

type Document struct {
	Version int     `yaml:"version" json:"version"`
	Schema  string  `yaml:"schema" json:"schema"`
	Tables  []Table `yaml:"tables" json:"tables"`
}
type Table struct {
	Name        string       `yaml:"name" json:"name"`
	Columns     []Column     `yaml:"columns" json:"columns"`
	PrimaryKey  []string     `yaml:"primary_key,omitempty" json:"primary_key,omitempty"`
	Unique      []Unique     `yaml:"unique,omitempty" json:"unique,omitempty"`
	Indexes     []Index      `yaml:"indexes,omitempty" json:"indexes,omitempty"`
	ForeignKeys []ForeignKey `yaml:"foreign_keys,omitempty" json:"foreign_keys,omitempty"`
	Checks      []Check      `yaml:"checks,omitempty" json:"checks,omitempty"`
}
type Column struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type" json:"type"`
	Nullable bool   `yaml:"nullable" json:"nullable"`
	// Default is a restricted, canonical default expression; see SCHEMA.md.
	Default string `yaml:"default,omitempty" json:"default,omitempty"`
	// Identity is "always" or "by_default" for GENERATED ... AS IDENTITY.
	Identity string `yaml:"identity,omitempty" json:"identity,omitempty"`
}
type Index struct {
	Name    string   `yaml:"name" json:"name"`
	Columns []string `yaml:"columns" json:"columns"`
	Unique  bool     `yaml:"unique,omitempty" json:"unique,omitempty"`
}

// Unique is a named UNIQUE table constraint. Its backing index shares the name.
type Unique struct {
	Name    string   `yaml:"name" json:"name"`
	Columns []string `yaml:"columns" json:"columns"`
}

// ForeignKey references a primary key or unique column set of a table in the
// same schema. Empty actions mean NO ACTION.
type ForeignKey struct {
	Name       string    `yaml:"name" json:"name"`
	Columns    []string  `yaml:"columns" json:"columns"`
	References Reference `yaml:"references" json:"references"`
	OnDelete   string    `yaml:"on_delete,omitempty" json:"on_delete,omitempty"`
	OnUpdate   string    `yaml:"on_update,omitempty" json:"on_update,omitempty"`
}
type Reference struct {
	Table   string   `yaml:"table" json:"table"`
	Columns []string `yaml:"columns" json:"columns"`
}

// Load accepts exactly one YAML document, with no aliases, implicit scalar
// coercions or unknown fields. It returns a validated, canonical copy.
func Load(r io.Reader) (Document, error) {
	var d Document
	if err := strictyaml.Decode(r, &d); err != nil {
		return d, err
	}
	return Normalize(d)
}

var identifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func validName(s string) bool {
	return len(s) <= 63 && identifier.MatchString(s) && !strings.HasPrefix(s, "pg_") && !strings.HasPrefix(s, "forgeschema_") && s != "information_schema"
}

var types = map[string]string{
	"smallint": "smallint", "int2": "smallint", "integer": "integer", "int": "integer", "int4": "integer",
	"bigint": "bigint", "int8": "bigint", "text": "text", "boolean": "boolean", "bool": "boolean",
	"uuid": "uuid", "date": "date", "timestamp": "timestamp without time zone", "timestamp without time zone": "timestamp without time zone",
	"timestamptz": "timestamp with time zone", "timestamp with time zone": "timestamp with time zone",
	"jsonb": "jsonb", "bytea": "bytea", "real": "real", "float4": "real", "double precision": "double precision", "float8": "double precision",
	"varchar": "character varying", "character varying": "character varying", "char": "character(1)", "character": "character(1)",
	"numeric": "numeric", "decimal": "numeric",
}

var (
	spaceBeforeParen = regexp.MustCompile(`\s*\(\s*`)
	spaceAroundComma = regexp.MustCompile(`\s*,\s*`)
	spaceBeforeClose = regexp.MustCompile(`\s*\)`)
	lengthType       = regexp.MustCompile(`^(varchar|character varying|char|character)\((\d+)\)$`)
	numericType      = regexp.MustCompile(`^(numeric|decimal)\((\d+)(?:,(\d+))?\)$`)
)

// maxLength is PostgreSQL's limit for character type modifiers.
const maxLength = 10485760

// NormalizeType returns PostgreSQL's format_type spelling for a supported type.
func NormalizeType(raw string) (string, bool) {
	s := strings.Join(strings.Fields(strings.ToLower(raw)), " ")
	s = spaceBeforeParen.ReplaceAllString(s, "(")
	s = spaceAroundComma.ReplaceAllString(s, ",")
	s = spaceBeforeClose.ReplaceAllString(s, ")")
	if t, ok := types[s]; ok {
		return t, true
	}
	if m := lengthType.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[2])
		if err != nil || n < 1 || n > maxLength || (len(m[2]) > 1 && m[2][0] == '0') {
			return "", false
		}
		if m[1] == "varchar" || m[1] == "character varying" {
			return "character varying(" + m[2] + ")", true
		}
		return "character(" + m[2] + ")", true
	}
	if m := numericType.FindStringSubmatch(s); m != nil {
		p, err := strconv.Atoi(m[2])
		if err != nil || p < 1 || p > 1000 || (len(m[2]) > 1 && m[2][0] == '0') {
			return "", false
		}
		scale := "0"
		if m[3] != "" {
			scale = m[3]
		}
		sc, err := strconv.Atoi(scale)
		if err != nil || sc > p || (len(scale) > 1 && scale[0] == '0') {
			return "", false
		}
		return fmt.Sprintf("numeric(%d,%d)", p, sc), true
	}
	return "", false
}

// IntegerType reports smallint, integer and bigint.
func IntegerType(t string) bool { return t == "smallint" || t == "integer" || t == "bigint" }

// TextType reports text, character varying and character types.
func TextType(t string) bool {
	return t == "text" || strings.HasPrefix(t, "character varying") || strings.HasPrefix(t, "character(")
}
func numberType(t string) bool {
	return IntegerType(t) || t == "real" || t == "double precision" || strings.HasPrefix(t, "numeric")
}

// typeArgs returns the integer modifiers of a parameterized type such as
// numeric(10,2).
func typeArgs(t, base string) ([]int, bool) {
	if !strings.HasPrefix(t, base+"(") {
		return nil, false
	}
	var out []int
	for _, part := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(t, base+"("), ")"), ",") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}

// TypeLength returns the declared length of a character type, or 0.
func TypeLength(t string) int {
	i := strings.IndexByte(t, '(')
	if i < 0 || !TextType(t) {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSuffix(t[i+1:], ")"))
	return n
}

var (
	integerLiteral = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	decimalLiteral = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)
	// Backslashes are excluded: with standard_conforming_strings off they
	// would end the literal early and let a default inject SQL.
	stringLiteral = regexp.MustCompile(`^'([^'\\\x00]|'')*'$`)
)

var integerRange = map[string][2]int64{
	"smallint": {-32768, 32767}, "integer": {-2147483648, 2147483647}, "bigint": {-9223372036854775808, 9223372036854775807},
}

// Default functions accepted per type, in canonical lower-case spelling.
var defaultFunctions = map[string][]string{
	"timestamp with time zone":    {"now()", "current_timestamp"},
	"timestamp without time zone": {"now()", "current_timestamp", "localtimestamp"},
	"date":                        {"current_date"},
	"uuid":                        {"gen_random_uuid()"},
}

// NormalizeDefault validates a default expression for a normalized column type
// and returns its canonical spelling. Only literals and a few built-in
// functions are accepted, because they round-trip through PostgreSQL's catalog
// representation exactly.
func NormalizeDefault(typ, raw string) (string, error) {
	v := strings.TrimSpace(raw)
	lower := strings.ToLower(v)
	if strings.HasPrefix(v, "-") && strings.Trim(v, "-0.") == "" {
		return "", fmt.Errorf("negative zero is not a canonical default; use 0")
	}
	for _, f := range defaultFunctions[typ] {
		if lower == f {
			return f, nil
		}
	}
	switch {
	case IntegerType(typ):
		if !integerLiteral.MatchString(v) {
			return "", fmt.Errorf("default for %s must be an integer literal", typ)
		}
		n, err := strconv.ParseInt(v, 10, 64)
		r := integerRange[typ]
		if err != nil || n < r[0] || n > r[1] {
			return "", fmt.Errorf("default %s is out of range for %s", v, typ)
		}
		return v, nil
	case numberType(typ):
		if !decimalLiteral.MatchString(v) {
			return "", fmt.Errorf("default for %s must be a decimal literal such as 0 or -1.25", typ)
		}
		whole, fraction, _ := strings.Cut(strings.TrimPrefix(v, "-"), ".")
		if whole == "0" {
			whole = ""
		}
		if mod, ok := typeArgs(typ, "numeric"); ok && (len(fraction) > mod[1] || len(whole) > mod[0]-mod[1]) {
			return "", fmt.Errorf("default %s does not fit %s exactly", v, typ)
		}
		if (typ == "real" && len(whole) > 38) || (typ == "double precision" && len(whole) > 308) {
			return "", fmt.Errorf("default %s is out of range for %s", v, typ)
		}
		return v, nil
	case typ == "boolean":
		if lower != "true" && lower != "false" {
			return "", fmt.Errorf("default for boolean must be true or false")
		}
		return lower, nil
	case TextType(typ):
		if !stringLiteral.MatchString(v) {
			return "", fmt.Errorf("default for %s must be a single-quoted string literal such as 'active'", typ)
		}
		if n := TypeLength(typ); n > 0 && len([]rune(strings.ReplaceAll(v[1:len(v)-1], "''", "'"))) > n {
			return "", fmt.Errorf("default %s is longer than %s", v, typ)
		}
		return v, nil
	case typ == "jsonb":
		if v != "'{}'" && v != "'[]'" {
			return "", fmt.Errorf("default for jsonb must be '{}' or '[]'")
		}
		return v, nil
	}
	if fs := defaultFunctions[typ]; len(fs) > 0 {
		return "", fmt.Errorf("default for %s must be one of %s", typ, strings.Join(fs, ", "))
	}
	return "", fmt.Errorf("defaults are not supported for %s", typ)
}

// DefaultSQL renders a canonical default for use in DDL.
func DefaultSQL(d string) string {
	switch d {
	case "current_timestamp", "current_date", "localtimestamp":
		return strings.ToUpper(d)
	}
	return d
}

// VolatileDefault reports defaults PostgreSQL evaluates per row when adding a
// column, which rewrites the table instead of storing one value.
func VolatileDefault(d string) bool { return d == "gen_random_uuid()" }

var catalogCast = regexp.MustCompile(`^(.*)::(integer|bigint|numeric|text|character varying|bpchar|jsonb)$`)

// impliedCast reports the casts PostgreSQL adds to a supported default on its
// own, which do not change the value: integer/bigint/numeric around number
// literals, the column's own text type around string literals, and varchar
// on a text column. Any other
// cast (say 'ab '::bpchar on a text column, or '0.1'::real on double
// precision) changes the stored value and is not stripped.
func impliedCast(typ, cast string) bool {
	switch {
	case numberType(typ):
		return cast == "integer" || cast == "bigint" || cast == "numeric"
	case typ == "text":
		// A varchar column widened to text keeps its default's varchar cast.
		return cast == "text" || cast == "character varying"
	case strings.HasPrefix(typ, "character varying"):
		return cast == "character varying"
	case strings.HasPrefix(typ, "character("):
		return cast == "bpchar"
	}
	return typ == "jsonb" && cast == "jsonb"
}

// DefaultFromCatalog converts pg_get_expr output to the canonical default, or
// reports false when the expression is outside the supported grammar.
func DefaultFromCatalog(typ, expr string) (string, bool) {
	v := expr
	if m := catalogCast.FindStringSubmatch(v); m != nil {
		if !impliedCast(typ, m[2]) {
			return "", false
		}
		v = m[1]
		if numberType(typ) {
			if !stringLiteral.MatchString(v) {
				return "", false
			}
			v = v[1 : len(v)-1]
		}
	}
	d, err := NormalizeDefault(typ, v)
	if err != nil {
		return "", false
	}
	return d, true
}

// IdentitySequence is the deterministic sequence name of an identity column.
func IdentitySequence(table, column string) string { return table + "_" + column + "_seq" }

// Actions are canonical foreign key referential actions.
var actions = map[string]string{"": "", "no_action": "", "restrict": "restrict", "cascade": "cascade", "set_null": "set_null", "set_default": "set_default"}

// ActionSQL renders a canonical referential action.
func ActionSQL(a string) string {
	if a == "" {
		return "NO ACTION"
	}
	return strings.ToUpper(strings.ReplaceAll(a, "_", " "))
}

// Normalize validates even documents constructed directly in Go. It does not
// mutate the caller's slices. Table, column, constraint and index order is
// nonsemantic; key, constraint and index column order is preserved.
func Normalize(in Document) (Document, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return Document{}, err
	}
	var d Document
	if err = json.Unmarshal(b, &d); err != nil {
		return d, err
	}
	if d.Version != 1 {
		return d, fmt.Errorf("version must be 1")
	}
	if !validName(d.Schema) {
		return d, fmt.Errorf("invalid or reserved schema name %q", d.Schema)
	}
	if d.Tables == nil {
		return d, fmt.Errorf("tables is required (use [] for an empty schema)")
	}
	// relations holds every pg_class name in the schema: tables, indexes and
	// identity sequences, which PostgreSQL requires to be unique.
	relations := map[string]bool{}
	reserve := func(name, what string) error {
		if len(name) > 63 || relations[name] {
			return fmt.Errorf("%s name %q is too long or conflicts with another relation", what, name)
		}
		relations[name] = true
		return nil
	}
	for _, t := range d.Tables {
		if !validName(t.Name) {
			return d, fmt.Errorf("invalid or reserved table name %q", t.Name)
		}
		if relations[t.Name] {
			return d, fmt.Errorf("duplicate relation %q", t.Name)
		}
		relations[t.Name] = true
	}
	tableColumns := map[string]map[string]Column{}
	for ti := range d.Tables {
		t := &d.Tables[ti]
		if len(t.Columns) == 0 {
			return d, fmt.Errorf("table %s: at least one column is required", t.Name)
		}
		if len(t.Columns) > 1600 {
			return d, fmt.Errorf("table %s: at most 1600 columns are supported", t.Name)
		}
		cols := map[string]Column{}
		for ci := range t.Columns {
			c := &t.Columns[ci]
			if !validName(c.Name) {
				return d, fmt.Errorf("table %s: invalid column name %q", t.Name, c.Name)
			}
			if _, ok := cols[c.Name]; ok {
				return d, fmt.Errorf("table %s: duplicate column %q", t.Name, c.Name)
			}
			normalized, ok := NormalizeType(c.Type)
			if !ok {
				return d, fmt.Errorf("table %s column %s: unsupported type %q", t.Name, c.Name, c.Type)
			}
			c.Type = normalized
			if c.Default != "" {
				if c.Default, err = NormalizeDefault(c.Type, c.Default); err != nil {
					return d, fmt.Errorf("table %s column %s: %w", t.Name, c.Name, err)
				}
			}
			switch c.Identity {
			case "":
			case "always", "by_default":
				if !IntegerType(c.Type) || c.Nullable || c.Default != "" {
					return d, fmt.Errorf("table %s column %s: identity columns must be smallint, integer or bigint, not nullable and without a default", t.Name, c.Name)
				}
				if err := reserve(IdentitySequence(t.Name, c.Name), "identity sequence"); err != nil {
					return d, fmt.Errorf("table %s column %s: %w", t.Name, c.Name, err)
				}
			default:
				return d, fmt.Errorf("table %s column %s: identity must be always or by_default", t.Name, c.Name)
			}
			cols[c.Name] = *c
		}
		tableColumns[t.Name] = cols
		if len(t.PrimaryKey) > 0 {
			if err := references(t.Name+" primary key", t.PrimaryKey, cols, true); err != nil {
				return d, err
			}
			// A deterministic name avoids PostgreSQL's truncation/suffix rules.
			if err := reserve(t.Name+"_pkey", "primary key"); err != nil {
				return d, fmt.Errorf("table %s: %w", t.Name, err)
			}
		}
	}
	for ti := range d.Tables {
		t := &d.Tables[ti]
		cols := tableColumns[t.Name]
		for _, u := range t.Unique {
			if !validName(u.Name) {
				return d, fmt.Errorf("table %s: invalid unique constraint name %q", t.Name, u.Name)
			}
			if err := reserve(u.Name, "unique constraint"); err != nil {
				return d, fmt.Errorf("table %s: %w", t.Name, err)
			}
			if err := references(t.Name+" unique "+u.Name, u.Columns, cols, false); err != nil {
				return d, err
			}
		}
		for _, idx := range t.Indexes {
			if !validName(idx.Name) || relations[idx.Name] {
				return d, fmt.Errorf("table %s: invalid or conflicting index name %q", t.Name, idx.Name)
			}
			relations[idx.Name] = true
			if err := references(t.Name+" index "+idx.Name, idx.Columns, cols, false); err != nil {
				return d, err
			}
		}
	}
	byName := map[string]*Table{}
	for ti := range d.Tables {
		byName[d.Tables[ti].Name] = &d.Tables[ti]
	}
	for ti := range d.Tables {
		t := &d.Tables[ti]
		constraints := map[string]bool{}
		if len(t.PrimaryKey) > 0 {
			constraints[t.Name+"_pkey"] = true
		}
		for _, u := range t.Unique {
			constraints[u.Name] = true
		}
		for fi := range t.ForeignKeys {
			fk := &t.ForeignKeys[fi]
			label := t.Name + " foreign key " + fk.Name
			if !validName(fk.Name) || constraints[fk.Name] {
				return d, fmt.Errorf("table %s: invalid or conflicting foreign key name %q", t.Name, fk.Name)
			}
			constraints[fk.Name] = true
			if err := references(label, fk.Columns, tableColumns[t.Name], false); err != nil {
				return d, err
			}
			target, ok := byName[fk.References.Table]
			if !ok {
				return d, fmt.Errorf("%s: referenced table %q is not in the schema", label, fk.References.Table)
			}
			if err := references(label+" reference", fk.References.Columns, tableColumns[target.Name], false); err != nil {
				return d, err
			}
			if len(fk.Columns) != len(fk.References.Columns) {
				return d, fmt.Errorf("%s: column count differs from referenced column count", label)
			}
			for i, name := range fk.Columns {
				local, remote := tableColumns[t.Name][name], tableColumns[target.Name][fk.References.Columns[i]]
				if !compatible(local.Type, remote.Type) {
					return d, fmt.Errorf("%s: column %s (%s) is not compatible with %s.%s (%s)", label, name, local.Type, target.Name, remote.Name, remote.Type)
				}
			}
			if !uniquelyKeyed(*target, fk.References.Columns) {
				return d, fmt.Errorf("%s: referenced columns must be the primary key or a unique constraint/index of %s", label, target.Name)
			}
			var ok1, ok2 bool
			if fk.OnDelete, ok1 = actions[fk.OnDelete]; !ok1 {
				return d, fmt.Errorf("%s: unsupported on_delete %q", label, fk.OnDelete)
			}
			if fk.OnUpdate, ok2 = actions[fk.OnUpdate]; !ok2 {
				return d, fmt.Errorf("%s: unsupported on_update %q", label, fk.OnUpdate)
			}
			if fk.OnDelete == "set_null" || fk.OnUpdate == "set_null" {
				for _, name := range fk.Columns {
					if !tableColumns[t.Name][name].Nullable {
						return d, fmt.Errorf("%s: set_null requires nullable column %s", label, name)
					}
				}
			}
		}
		for ci := range t.Checks {
			c := &t.Checks[ci]
			if !validName(c.Name) || constraints[c.Name] {
				return d, fmt.Errorf("table %s: invalid or conflicting check constraint name %q", t.Name, c.Name)
			}
			constraints[c.Name] = true
			if c.Expression, err = NormalizeCheck(tableColumns[t.Name], c.Expression); err != nil {
				return d, fmt.Errorf("table %s check %s: %w", t.Name, c.Name, err)
			}
		}
	}
	for ti := range d.Tables {
		t := &d.Tables[ti]
		sort.Slice(t.Columns, func(i, j int) bool { return t.Columns[i].Name < t.Columns[j].Name })
		sort.Slice(t.Indexes, func(i, j int) bool { return t.Indexes[i].Name < t.Indexes[j].Name })
		sort.Slice(t.Unique, func(i, j int) bool { return t.Unique[i].Name < t.Unique[j].Name })
		sort.Slice(t.ForeignKeys, func(i, j int) bool { return t.ForeignKeys[i].Name < t.ForeignKeys[j].Name })
		sort.Slice(t.Checks, func(i, j int) bool { return t.Checks[i].Name < t.Checks[j].Name })
	}
	sort.Slice(d.Tables, func(i, j int) bool { return d.Tables[i].Name < d.Tables[j].Name })
	return d, nil
}

// compatible accepts identical types and pairs within the integer,
// varchar/text, char and numeric families, which PostgreSQL compares with a default btree equality operator.
func compatible(a, b string) bool {
	family := func(t string) string {
		switch {
		case IntegerType(t):
			return "integer"
		case t == "text" || strings.HasPrefix(t, "character varying"):
			return "varchar"
		case strings.HasPrefix(t, "character("):
			return "char"
		case strings.HasPrefix(t, "numeric"):
			return "numeric"
		}
		return t
	}
	return family(a) == family(b)
}

// uniquelyKeyed reports whether columns, as a set, match the table's primary
// key, a unique constraint or a unique index, as PostgreSQL requires.
func uniquelyKeyed(t Table, columns []string) bool {
	key := setKey(columns)
	if len(t.PrimaryKey) > 0 && setKey(t.PrimaryKey) == key {
		return true
	}
	for _, u := range t.Unique {
		if setKey(u.Columns) == key {
			return true
		}
	}
	for _, idx := range t.Indexes {
		if idx.Unique && setKey(idx.Columns) == key {
			return true
		}
	}
	return false
}
func setKey(ns []string) string {
	s := append([]string{}, ns...)
	sort.Strings(s)
	return strings.Join(s, "\x00")
}
func references(label string, names []string, cols map[string]Column, pk bool) error {
	if len(names) > 32 {
		return fmt.Errorf("%s: at most 32 columns are supported", label)
	}
	if len(names) == 0 {
		return fmt.Errorf("%s: columns cannot be empty", label)
	}
	seen := map[string]bool{}
	for _, name := range names {
		c, ok := cols[name]
		if !ok {
			return fmt.Errorf("%s: unknown column %q", label, name)
		}
		if seen[name] {
			return fmt.Errorf("%s: repeated column %q", label, name)
		}
		seen[name] = true
		if pk && c.Nullable {
			return fmt.Errorf("%s: column %s cannot be nullable", label, name)
		}
	}
	return nil
}

func (d Document) CanonicalJSON() ([]byte, error) {
	n, err := Normalize(d)
	if err != nil {
		return nil, err
	}
	return json.Marshal(n)
}
func (d Document) Fingerprint() (string, error) {
	b, err := d.CanonicalJSON()
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}
