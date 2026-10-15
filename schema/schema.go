// Package schema reads and validates ForgeSchema's versioned desired schema.
package schema

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const MaxBytes = 1 << 20

type Document struct {
	Version int     `yaml:"version" json:"version"`
	Schema  string  `yaml:"schema" json:"schema"`
	Tables  []Table `yaml:"tables" json:"tables"`
}
type Table struct {
	Name       string   `yaml:"name" json:"name"`
	Columns    []Column `yaml:"columns" json:"columns"`
	PrimaryKey []string `yaml:"primary_key,omitempty" json:"primary_key,omitempty"`
	Indexes    []Index  `yaml:"indexes,omitempty" json:"indexes,omitempty"`
}
type Column struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type" json:"type"`
	Nullable bool   `yaml:"nullable" json:"nullable"`
}
type Index struct {
	Name    string   `yaml:"name" json:"name"`
	Columns []string `yaml:"columns" json:"columns"`
}

// Load accepts exactly one YAML document, with no aliases, implicit scalar
// coercions or unknown fields. It returns a validated, canonical copy.
func Load(r io.Reader) (Document, error) {
	var d Document
	b, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return d, err
	}
	if len(b) > MaxBytes {
		return d, fmt.Errorf("schema exceeds %d bytes", MaxBytes)
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var n yaml.Node
	if err = dec.Decode(&n); err != nil {
		return d, fmt.Errorf("read schema: %w", err)
	}
	if len(n.Content) != 1 {
		return d, fmt.Errorf("schema must be a mapping")
	}
	if err = checkNode(n.Content[0], reflect.TypeOf(d), "schema", 0); err != nil {
		return d, err
	}
	var extra yaml.Node
	if err = dec.Decode(&extra); err != io.EOF {
		return d, fmt.Errorf("expected exactly one YAML document")
	}
	strict := yaml.NewDecoder(bytes.NewReader(b))
	strict.KnownFields(true)
	if err = strict.Decode(&d); err != nil {
		return d, fmt.Errorf("decode schema: %w", err)
	}
	return Normalize(d)
}

func checkNode(n *yaml.Node, t reflect.Type, path string, depth int) error {
	if depth > 32 {
		return fmt.Errorf("%s: nesting too deep", path)
	}
	if n.Anchor != "" || n.Kind == yaml.AliasNode {
		return fmt.Errorf("%s: YAML anchors and aliases are unsupported", path)
	}
	switch t.Kind() {
	case reflect.Struct:
		if n.Kind != yaml.MappingNode || n.Tag != "!!map" {
			return fmt.Errorf("%s: expected mapping", path)
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			fields[strings.Split(f.Tag.Get("yaml"), ",")[0]] = f.Type
		}
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Anchor != "" || k.Kind == yaml.AliasNode {
				return fmt.Errorf("%s: YAML anchors and aliases are unsupported", path)
			}
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
				return fmt.Errorf("%s: expected string field name", path)
			}
			ft, ok := fields[k.Value]
			if !ok {
				return fmt.Errorf("%s: unknown field %q", path, k.Value)
			}
			if seen[k.Value] {
				return fmt.Errorf("%s: duplicate field %q", path, k.Value)
			}
			seen[k.Value] = true
			if err := checkNode(v, ft, path+"."+k.Value, depth+1); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if n.Kind != yaml.SequenceNode || n.Tag != "!!seq" {
			return fmt.Errorf("%s: expected sequence", path)
		}
		for i, v := range n.Content {
			if err := checkNode(v, t.Elem(), fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
				return err
			}
		}
	default:
		tag := "!!str"
		if t.Kind() == reflect.Int {
			tag = "!!int"
		}
		if t.Kind() == reflect.Bool {
			tag = "!!bool"
		}
		if n.Kind != yaml.ScalarNode || n.Tag != tag {
			return fmt.Errorf("%s: expected %s scalar", path, strings.TrimPrefix(tag, "!!"))
		}
	}
	return nil
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
}

// Normalize validates even documents constructed directly in Go. It does not
// mutate the caller's slices. Table, column and index order is nonsemantic;
// primary key and index column order is preserved.
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
	relations := map[string]bool{}
	for _, t := range d.Tables {
		if !validName(t.Name) {
			return d, fmt.Errorf("invalid or reserved table name %q", t.Name)
		}
		if relations[t.Name] {
			return d, fmt.Errorf("duplicate relation %q", t.Name)
		}
		relations[t.Name] = true
	}
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
			normalized, ok := types[strings.Join(strings.Fields(strings.ToLower(c.Type)), " ")]
			if !ok {
				return d, fmt.Errorf("table %s column %s: unsupported type %q", t.Name, c.Name, c.Type)
			}
			c.Type = normalized
			cols[c.Name] = *c
		}
		if len(t.PrimaryKey) > 0 {
			if err := references(t.Name+" primary key", t.PrimaryKey, cols, true); err != nil {
				return d, err
			}
			// A deterministic name avoids PostgreSQL's truncation/suffix rules.
			name := t.Name + "_pkey"
			if len(name) > 63 || relations[name] {
				return d, fmt.Errorf("table %s: primary key relation name %q is too long or conflicts", t.Name, name)
			}
			relations[name] = true
		}
	}
	for ti := range d.Tables {
		t := &d.Tables[ti]
		cols := map[string]Column{}
		for _, c := range t.Columns {
			cols[c.Name] = c
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
		sort.Slice(t.Columns, func(i, j int) bool { return t.Columns[i].Name < t.Columns[j].Name })
		sort.Slice(t.Indexes, func(i, j int) bool { return t.Indexes[i].Name < t.Indexes[j].Name })
	}
	sort.Slice(d.Tables, func(i, j int) bool { return d.Tables[i].Name < d.Tables[j].Name })
	return d, nil
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
