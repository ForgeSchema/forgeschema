// Package plan builds deterministic, reviewable PostgreSQL migration plans.
// It does not connect to databases or execute SQL.
package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/ForgeSchema/forgeschema/internal/strictyaml"
	"github.com/ForgeSchema/forgeschema/schema"
)

const FormatVersion = 1

// Rename identifies a column rename on an existing table. Chains, swaps and
// reuse of the source name in the target are deliberately unsupported.
type Rename struct {
	Table string `yaml:"table" json:"table"`
	From  string `yaml:"from" json:"from"`
	To    string `yaml:"to" json:"to"`
}
type RenameFile struct {
	Version int      `yaml:"version" json:"version"`
	Columns []Rename `yaml:"columns" json:"columns"`
}
type Operation struct {
	Kind        string `json:"kind"`
	Table       string `json:"table"`
	Subject     string `json:"subject,omitempty"`
	SQL         string `json:"sql"`
	Explanation string `json:"explanation"`
}
type Blocker struct {
	Object string `json:"object"`
	Reason string `json:"reason"`
}
type Plan struct {
	FormatVersion     int             `json:"format_version"`
	Source            schema.Document `json:"source"`
	Target            schema.Document `json:"target"`
	SourceFingerprint string          `json:"source_fingerprint"`
	TargetFingerprint string          `json:"target_fingerprint"`
	Renames           []Rename        `json:"renames"`
	Status            string          `json:"status"`
	Operations        []Operation     `json:"operations"`
	Blocked           []Blocker       `json:"blocked"`
}

func LoadRenames(r io.Reader) ([]Rename, error) {
	var f RenameFile
	if err := strictyaml.Decode(r, &f); err != nil {
		return nil, err
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("rename version must be 1")
	}
	if f.Columns == nil {
		return nil, fmt.Errorf("rename columns is required (use [] for no renames)")
	}
	return f.Columns, nil
}
func quote(s string) string        { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func qualified(s, t string) string { return quote(s) + "." + quote(t) }
func names(ns []string) string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = quote(n)
	}
	return strings.Join(out, ", ")
}
func tables(d schema.Document) map[string]schema.Table {
	m := map[string]schema.Table{}
	for _, t := range d.Tables {
		m[t.Name] = t
	}
	return m
}
func columns(t schema.Table) map[string]schema.Column {
	m := map[string]schema.Column{}
	for _, c := range t.Columns {
		m[c.Name] = c
	}
	return m
}
func indexes(t schema.Table) map[string]schema.Index {
	m := map[string]schema.Index{}
	for _, x := range t.Indexes {
		m[x.Name] = x
	}
	return m
}
func sameNames(a, b []string) bool { return strings.Join(a, "\x00") == strings.Join(b, "\x00") }

// Build validates and normalizes both schemas. A blocked plan contains NO
// operations, even if some requested changes would be supported in isolation.
func Build(source, target schema.Document, renames []Rename) (Plan, error) {
	var p Plan
	src, err := schema.Normalize(source)
	if err != nil {
		return p, fmt.Errorf("source: %w", err)
	}
	dst, err := schema.Normalize(target)
	if err != nil {
		return p, fmt.Errorf("target: %w", err)
	}
	if src.Schema != dst.Schema {
		return p, fmt.Errorf("source and target must name the same existing schema")
	}
	hints := append([]Rename{}, renames...)
	sort.Slice(hints, func(i, j int) bool {
		a, b := hints[i], hints[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	working, err := schema.Normalize(src)
	if err != nil {
		return p, err
	}
	sm, tm := tables(src), tables(dst)
	seenFrom, seenTo := map[string]bool{}, map[string]bool{}
	for _, r := range hints {
		st, ok := sm[r.Table]
		tt, targetOK := tm[r.Table]
		if !ok || !targetOK {
			return p, fmt.Errorf("rename table %q must exist in both source and target", r.Table)
		}
		sc, tc := columns(st), columns(tt)
		_, fromOK := sc[r.From]
		_, toOK := tc[r.To]
		_, occupied := sc[r.To]
		_, reused := tc[r.From]
		if r.From == r.To || !fromOK || !toOK || occupied || reused {
			return p, fmt.Errorf("rename %s.%s to %s: source must exist, target must be new, and old name must not be reused", r.Table, r.From, r.To)
		}
		fromKey, toKey := r.Table+"."+r.From, r.Table+"."+r.To
		if seenFrom[fromKey] || seenTo[toKey] {
			return p, fmt.Errorf("rename mappings must be one-to-one: %s", r.Table)
		}
		seenFrom[fromKey] = true
		seenTo[toKey] = true
	}
	// Apply explicit identities to a separate comparison copy. PostgreSQL adjusts
	// existing primary key and index column references when a column is renamed.
	for ti := range working.Tables {
		t := &working.Tables[ti]
		mapping := map[string]string{}
		for _, r := range hints {
			if r.Table == t.Name {
				mapping[r.From] = r.To
			}
		}
		for ci := range t.Columns {
			if n, ok := mapping[t.Columns[ci].Name]; ok {
				t.Columns[ci].Name = n
			}
		}
		for i, n := range t.PrimaryKey {
			if next, ok := mapping[n]; ok {
				t.PrimaryKey[i] = next
			}
		}
		for i := range t.Indexes {
			for j, n := range t.Indexes[i].Columns {
				if next, ok := mapping[n]; ok {
					t.Indexes[i].Columns[j] = next
				}
			}
		}
	}
	wm := tables(working)
	p = Plan{FormatVersion: FormatVersion, Source: src, Target: dst, Renames: hints, Status: "ready", Operations: []Operation{}, Blocked: []Blocker{}}
	if p.SourceFingerprint, err = src.Fingerprint(); err != nil {
		return Plan{}, err
	}
	if p.TargetFingerprint, err = dst.Fingerprint(); err != nil {
		return Plan{}, err
	}
	var createOps, renameOps, addOps, indexOps []Operation
	block := func(object, reason string) { p.Blocked = append(p.Blocked, Blocker{object, reason}) }
	for _, st := range working.Tables {
		if _, ok := tm[st.Name]; !ok {
			block(st.Name, "dropping or implicitly renaming a table is unsupported")
		}
	}
	for _, tt := range dst.Tables {
		st, exists := wm[tt.Name]
		if !exists {
			definitions := []string{}
			for _, c := range tt.Columns {
				nullability := " NOT NULL"
				if c.Nullable {
					nullability = " NULL"
				}
				definitions = append(definitions, quote(c.Name)+" "+c.Type+nullability)
			}
			if len(tt.PrimaryKey) > 0 {
				definitions = append(definitions, "CONSTRAINT "+quote(tt.Name+"_pkey")+" PRIMARY KEY ("+names(tt.PrimaryKey)+")")
			}
			createOps = append(createOps, Operation{Kind: "create_table", Table: tt.Name, SQL: "CREATE TABLE " + qualified(dst.Schema, tt.Name) + " (" + strings.Join(definitions, ", ") + ");", Explanation: "Create a new table with its declared columns and primary key; indexes follow separately."})
		} else {
			sc, tc := columns(st), columns(tt)
			for _, c := range st.Columns {
				if _, ok := tc[c.Name]; !ok {
					block(tt.Name+"."+c.Name, "dropping or implicitly renaming a column is unsupported; provide an explicit rename mapping")
				}
			}
			for _, c := range tt.Columns {
				old, ok := sc[c.Name]
				if !ok {
					if !c.Nullable {
						block(tt.Name+"."+c.Name, "adding a required column to an existing table needs a staged data migration")
						continue
					}
					addOps = append(addOps, Operation{Kind: "add_column", Table: tt.Name, Subject: c.Name, SQL: "ALTER TABLE " + qualified(dst.Schema, tt.Name) + " ADD COLUMN " + quote(c.Name) + " " + c.Type + " NULL;", Explanation: "Add a nullable column without a default; existing rows receive NULL. Requires a table lock."})
				} else {
					if old.Type != c.Type {
						block(tt.Name+"."+c.Name, "type conversions are unsupported")
					}
					if old.Nullable != c.Nullable {
						block(tt.Name+"."+c.Name, "changing nullability is unsupported")
					}
				}
			}
			if !sameNames(st.PrimaryKey, tt.PrimaryKey) {
				block(tt.Name, "changing an existing table's primary key is unsupported")
			}
			targetIndexes := indexes(tt)
			for _, idx := range st.Indexes {
				want, ok := targetIndexes[idx.Name]
				if !ok {
					block(tt.Name+" index "+idx.Name, "dropping or renaming an index is unsupported")
				} else if !sameNames(idx.Columns, want.Columns) {
					block(tt.Name+" index "+idx.Name, "changing an index definition requires rebuilding it, which is unsupported")
				}
			}
		}
		oldIndexes := indexes(st)
		for _, idx := range tt.Indexes {
			if _, ok := oldIndexes[idx.Name]; ok {
				continue
			}
			indexOps = append(indexOps, Operation{Kind: "create_index", Table: tt.Name, Subject: idx.Name, SQL: "CREATE INDEX " + quote(idx.Name) + " ON " + qualified(dst.Schema, tt.Name) + " (" + names(idx.Columns) + ");", Explanation: "Create an ordinary btree index after table and column changes. This is not concurrent and can block writes."})
		}
	}
	for _, r := range hints {
		renameOps = append(renameOps, Operation{Kind: "rename_column", Table: r.Table, Subject: r.From, SQL: "ALTER TABLE " + qualified(dst.Schema, r.Table) + " RENAME COLUMN " + quote(r.From) + " TO " + quote(r.To) + ";", Explanation: "Explicitly rename " + r.From + " to " + r.To + "; PostgreSQL retains column data and updates existing key/index references. Requires a table lock."})
	}
	sort.Slice(p.Blocked, func(i, j int) bool {
		a, b := p.Blocked[i], p.Blocked[j]
		if a.Object != b.Object {
			return a.Object < b.Object
		}
		return a.Reason < b.Reason
	})
	if len(p.Blocked) > 0 {
		p.Status = "blocked"
		return p, nil
	}
	p.Operations = append(p.Operations, createOps...)
	p.Operations = append(p.Operations, renameOps...)
	p.Operations = append(p.Operations, addOps...)
	p.Operations = append(p.Operations, indexOps...)
	return p, nil
}

// Validate rebuilds the plan from its canonical schemas and rename intent and
// rejects altered SQL, operations, explanations, status or fingerprints. It is
// an internal consistency check, not a signature or proof the database matches.
func (p Plan) Validate() error {
	rebuilt, err := Build(p.Source, p.Target, p.Renames)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(p, rebuilt) {
		return fmt.Errorf("plan differs from the canonical plan rebuilt from its schemas and rename mappings")
	}
	return nil
}
func (p Plan) JSON() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(p, "", "  ")
}

// Summary presents a human-readable review. Blocked plans include reasons and
// deliberately have no SQL or partial operation list.
func (p Plan) Summary() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "ForgeSchema plan: %s\nSource: %s\nTarget: %s\n", p.Status, p.SourceFingerprint, p.TargetFingerprint)
	for _, v := range p.Blocked {
		fmt.Fprintf(&b, "Blocked %s: %s\n", v.Object, v.Reason)
	}
	if p.Status == "ready" && len(p.Operations) == 0 {
		fmt.Fprintln(&b, "No schema changes.")
	}
	for i, op := range p.Operations {
		fmt.Fprintf(&b, "%d. %s\n%s\n", i+1, op.Explanation, op.SQL)
	}
	fmt.Fprintln(&b, "Review only: ForgeSchema does not execute plans in this checkpoint.")
	return b.String(), nil
}
