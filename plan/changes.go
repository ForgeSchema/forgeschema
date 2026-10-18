package plan

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ForgeSchema/forgeschema/internal/strictyaml"
	"github.com/ForgeSchema/forgeschema/schema"
)

// Drop names one object the plan may delete. Without a drop, removing an
// object from the target blocks the plan. Table alone drops the whole table;
// otherwise exactly one of the other fields names an object on that table.
type Drop struct {
	Table      string `yaml:"table" json:"table"`
	Column     string `yaml:"column,omitempty" json:"column,omitempty"`
	Index      string `yaml:"index,omitempty" json:"index,omitempty"`
	Unique     string `yaml:"unique,omitempty" json:"unique,omitempty"`
	ForeignKey string `yaml:"foreign_key,omitempty" json:"foreign_key,omitempty"`
	Check      string `yaml:"check,omitempty" json:"check,omitempty"`
}

// Conversion allows a type change that is not a widening on one column,
// named as in the target. PostgreSQL converts the stored values.
type Conversion struct {
	Table  string `yaml:"table" json:"table"`
	Column string `yaml:"column" json:"column"`
}

// ChangeFile is the explicit change input: renames, drops and conversions.
// A file with only columns is the 0.1 rename file.
type ChangeFile struct {
	Version     int          `yaml:"version" json:"version"`
	Columns     []Rename     `yaml:"columns,omitempty" json:"columns,omitempty"`
	Drops       []Drop       `yaml:"drops,omitempty" json:"drops,omitempty"`
	Conversions []Conversion `yaml:"conversions,omitempty" json:"conversions,omitempty"`
}

// Changes is the parsed explicit change input.
type Changes struct {
	Renames     []Rename
	Drops       []Drop
	Conversions []Conversion
}

// LoadChanges reads a change file. columns may be omitted when drops or
// conversions are given.
func LoadChanges(r io.Reader) (Changes, error) {
	var f ChangeFile
	if err := strictyaml.Decode(r, &f); err != nil {
		return Changes{}, err
	}
	if f.Version != 1 {
		return Changes{}, fmt.Errorf("change file version must be 1")
	}
	if f.Columns == nil && f.Drops == nil && f.Conversions == nil {
		return Changes{}, fmt.Errorf("change file needs columns, drops or conversions (use columns: [] for no renames)")
	}
	if f.Columns == nil {
		f.Columns = []Rename{}
	}
	return Changes{Renames: f.Columns, Drops: f.Drops, Conversions: f.Conversions}, nil
}

// kind returns what a drop names, and its name.
func (d Drop) kind() (string, string, error) {
	var kind, name string
	for _, f := range []struct{ kind, name string }{{"column", d.Column}, {"index", d.Index}, {"unique", d.Unique}, {"foreign_key", d.ForeignKey}, {"check", d.Check}} {
		if f.name == "" {
			continue
		}
		if kind != "" {
			return "", "", fmt.Errorf("drop on table %s names more than one object", d.Table)
		}
		kind, name = f.kind, f.name
	}
	if kind == "" {
		return "table", d.Table, nil
	}
	return kind, name, nil
}

func (d Drop) String() string {
	kind, name, _ := d.kind()
	if kind == "table" {
		return "table " + d.Table
	}
	if kind == "column" {
		return "column " + d.Table + "." + name
	}
	return strings.ReplaceAll(kind, "_", " ") + " " + name + " on " + d.Table
}

var dropOrder = map[string]int{"foreign_key": 0, "check": 1, "unique": 2, "index": 3, "column": 4, "table": 5}

func sortDrops(drops []Drop) []Drop {
	out := append([]Drop{}, drops...)
	sort.Slice(out, func(i, j int) bool {
		ki, ni, _ := out[i].kind()
		kj, nj, _ := out[j].kind()
		if dropOrder[ki] != dropOrder[kj] {
			return dropOrder[ki] < dropOrder[kj]
		}
		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}
		return ni < nj
	})
	return out
}

func sortConversions(cs []Conversion) []Conversion {
	out := append([]Conversion{}, cs...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}
		return out[i].Column < out[j].Column
	})
	return out
}

// checkDrops validates drops against the source and target. A dropped
// column may be absent from the target or reused there as a rename's new
// name; every other object must be absent from the target.
func checkDrops(drops []Drop, sm, tm map[string]schema.Table, hints []Rename) error {
	renamedTo, renamedFrom := map[string]bool{}, map[string]bool{}
	for _, r := range hints {
		renamedTo[r.Table+"."+r.To] = true
		renamedFrom[r.Table+"."+r.From] = true
	}
	seen := map[string]bool{}
	droppedTables := map[string]bool{}
	for _, d := range drops {
		kind, name, err := d.kind()
		if err != nil {
			return err
		}
		if seen[d.String()] {
			return fmt.Errorf("drop %s is listed twice", d)
		}
		seen[d.String()] = true
		st, inSource := sm[d.Table]
		tt, inTarget := tm[d.Table]
		if kind == "table" {
			if !inSource || inTarget {
				return fmt.Errorf("drop %s: the table must exist in the source and be absent from the target", d)
			}
			droppedTables[d.Table] = true
			continue
		}
		if !inSource || !inTarget {
			return fmt.Errorf("drop %s: the table must exist in both schemas; to drop the table, list only its name", d)
		}
		var inS, inT bool
		switch kind {
		case "column":
			_, inS = columns(st)[name]
			_, inT = columns(tt)[name]
			if renamedFrom[d.Table+"."+name] {
				return fmt.Errorf("drop %s: the column is renamed, not dropped", d)
			}
			if renamedTo[d.Table+"."+name] {
				inT = false // its name is reused by the renamed column
			}
		case "index":
			_, inS = indexes(st)[name]
			_, inT = indexes(tt)[name]
		case "unique":
			_, inS = uniques(st)[name]
			_, inT = uniques(tt)[name]
		case "foreign_key":
			_, inS = foreignKeys(st)[name]
			_, inT = foreignKeys(tt)[name]
		case "check":
			for _, c := range st.Checks {
				inS = inS || c.Name == name
			}
			for _, c := range tt.Checks {
				inT = inT || c.Name == name
			}
		}
		if !inS || inT {
			return fmt.Errorf("drop %s: it must exist in the source and be absent from the target", d)
		}
	}
	return nil
}

func checkConversions(cs []Conversion, sm, tm map[string]schema.Table, hints []Rename) error {
	seen := map[string]bool{}
	for _, c := range cs {
		key := c.Table + "." + c.Column
		if seen[key] {
			return fmt.Errorf("conversion %s is listed twice", key)
		}
		seen[key] = true
		st, ok1 := sm[c.Table]
		tt, ok2 := tm[c.Table]
		if !ok1 || !ok2 {
			return fmt.Errorf("conversion %s: the table must exist in both schemas", key)
		}
		target, ok := columns(tt)[c.Column]
		if !ok {
			return fmt.Errorf("conversion %s: the column must exist in the target", key)
		}
		from := c.Column
		for _, r := range hints {
			if r.Table == c.Table && r.To == c.Column {
				from = r.From
			}
		}
		source, ok := columns(st)[from]
		if !ok || source.Type == target.Type {
			return fmt.Errorf("conversion %s: the column must exist in the source with a different type", key)
		}
	}
	return nil
}

// dropFromWorking removes dropped objects from the comparison copy, so the
// comparison sees them as intended.
func dropFromWorking(working *schema.Document, drops []Drop) {
	dropped := map[string]bool{}
	for _, d := range drops {
		kind, name, _ := d.kind()
		dropped[kind+"\x00"+d.Table+"\x00"+name] = true
	}
	var keep []schema.Table
	for _, t := range working.Tables {
		if dropped["table\x00"+t.Name+"\x00"+t.Name] {
			continue
		}
		var cols []schema.Column
		for _, c := range t.Columns {
			if !dropped["column\x00"+t.Name+"\x00"+c.Name] {
				cols = append(cols, c)
			}
		}
		t.Columns = cols
		var idx []schema.Index
		for _, x := range t.Indexes {
			if !dropped["index\x00"+t.Name+"\x00"+x.Name] {
				idx = append(idx, x)
			}
		}
		t.Indexes = idx
		var us []schema.Unique
		for _, u := range t.Unique {
			if !dropped["unique\x00"+t.Name+"\x00"+u.Name] {
				us = append(us, u)
			}
		}
		t.Unique = us
		var fks []schema.ForeignKey
		for _, fk := range t.ForeignKeys {
			if !dropped["foreign_key\x00"+t.Name+"\x00"+fk.Name] {
				fks = append(fks, fk)
			}
		}
		t.ForeignKeys = fks
		var cks []schema.Check
		for _, c := range t.Checks {
			if !dropped["check\x00"+t.Name+"\x00"+c.Name] {
				cks = append(cks, c)
			}
		}
		t.Checks = cks
		keep = append(keep, t)
	}
	working.Tables = keep
}

// dropOperations returns the SQL for drops, in dependency order: foreign
// keys, checks, unique constraints, indexes, columns, then tables. Foreign
// keys from one dropped table to another are dropped first, so the tables
// can go in any order.
func dropOperations(src schema.Document, drops []Drop, block func(object, reason string)) []Operation {
	sm := tables(src)
	droppedTables, droppedColumns, droppedObjects := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, d := range drops {
		kind, name, _ := d.kind()
		switch kind {
		case "table":
			droppedTables[d.Table] = true
		case "column":
			droppedColumns[d.Table+"."+name] = true
		default:
			droppedObjects[kind+"\x00"+d.Table+"\x00"+name] = true
		}
	}
	alter := func(table string) string { return "ALTER TABLE " + qualified(src.Schema, table) }
	var ops []Operation
	// A dropped table's foreign keys would go with it, but they are removed
	// first, so that the keys and columns they reference can be dropped
	// before the table, and the tables in any order.
	for _, t := range src.Tables {
		if !droppedTables[t.Name] {
			continue
		}
		for _, fk := range t.ForeignKeys {
			if fk.References.Table != t.Name {
				ops = append(ops, Operation{Kind: "drop_foreign_key", Table: t.Name, Subject: fk.Name, SQL: alter(t.Name) + " DROP CONSTRAINT " + quote(fk.Name) + ";", Explanation: "Remove foreign key " + fk.Name + " of table " + t.Name + ", which this plan drops, before the other drops."})
			}
		}
	}
	// PostgreSQL ties a foreign key to one unique index of the referenced
	// columns, and the schema model does not record which, so no unique
	// constraint or unique index a kept foreign key could be using is dropped.
	keptReference := func(table string, cols []string) string {
		for _, t := range src.Tables {
			if droppedTables[t.Name] {
				continue
			}
			for _, fk := range t.ForeignKeys {
				if fk.References.Table == table && !droppedObjects["foreign_key\x00"+t.Name+"\x00"+fk.Name] && sameSet(fk.References.Columns, cols) {
					return fk.Name
				}
			}
		}
		return ""
	}
	for _, d := range drops {
		kind, name, _ := d.kind()
		var cols []string
		switch kind {
		case "unique":
			cols = uniques(sm[d.Table])[name].Columns
		case "index":
			if x := indexes(sm[d.Table])[name]; x.Unique {
				cols = x.Columns
			}
		}
		if cols == nil {
			continue
		}
		if fk := keptReference(d.Table, cols); fk != "" {
			block(d.Table+" "+strings.ReplaceAll(kind, "_", " ")+" "+name, "foreign key "+fk+" references these columns and may depend on this key; drop the foreign key too, or keep the key")
		}
	}
	for _, d := range drops {
		kind, name, _ := d.kind()
		st := sm[d.Table]
		switch kind {
		case "foreign_key":
			ops = append(ops, Operation{Kind: "drop_foreign_key", Table: d.Table, Subject: name, SQL: alter(d.Table) + " DROP CONSTRAINT " + quote(name) + ";", Explanation: "Drop foreign key " + name + ". Rows are kept; references are no longer enforced. Requires a brief lock on this table and " + foreignKeys(st)[name].References.Table + "."})
		case "check":
			ops = append(ops, Operation{Kind: "drop_check", Table: d.Table, Subject: name, SQL: alter(d.Table) + " DROP CONSTRAINT " + quote(name) + ";", Explanation: "Drop check constraint " + name + ". Rows are kept; the rule is no longer enforced. Metadata-only; requires a brief table lock."})
		case "unique":
			ops = append(ops, Operation{Kind: "drop_unique", Table: d.Table, Subject: name, SQL: alter(d.Table) + " DROP CONSTRAINT " + quote(name) + ";", Explanation: "Drop unique constraint " + name + " and its index. Rows are kept; duplicates become possible. Requires a brief table lock."})
		case "index":
			ops = append(ops, Operation{Kind: "drop_index", Table: d.Table, Subject: name, SQL: "DROP INDEX " + qualified(src.Schema, name) + ";", Explanation: "Drop index " + name + ". Rows are kept; queries that used it may get slower. Requires a brief table lock."})
		case "column":
			ops = append(ops, Operation{Kind: "drop_column", Table: d.Table, Subject: name, SQL: alter(d.Table) + " DROP COLUMN " + quote(name) + ";", Explanation: "Permanently delete column " + d.Table + "." + name + " and the data in it. PostgreSQL hides the column at once, without a rewrite, and fails if anything outside the plan depends on it. ForgeSchema cannot bring the data back. Requires a table lock."})
		case "table":
			ops = append(ops, Operation{Kind: "drop_table", Table: d.Table, SQL: "DROP TABLE " + qualified(src.Schema, d.Table) + ";", Explanation: "Permanently delete table " + d.Table + " with all its rows, indexes and constraints. PostgreSQL fails if anything outside the plan depends on it. ForgeSchema cannot bring the data back."})
		}
	}
	// Objects that use a dropped column must be dropped too, explicitly.
	for _, t := range src.Tables {
		if droppedTables[t.Name] {
			continue
		}
		uses := func(cols []string) string {
			for _, c := range cols {
				if droppedColumns[t.Name+"."+c] {
					return c
				}
			}
			return ""
		}
		if c := uses(t.PrimaryKey); c != "" {
			block(t.Name+"."+c, "the primary key uses this column; changing a primary key is unsupported")
		}
		for _, x := range t.Indexes {
			if c := uses(x.Columns); c != "" && !droppedObjects["index\x00"+t.Name+"\x00"+x.Name] {
				block(t.Name+" index "+x.Name, "it uses dropped column "+c+"; list it under drops too")
			}
		}
		for _, u := range t.Unique {
			if c := uses(u.Columns); c != "" && !droppedObjects["unique\x00"+t.Name+"\x00"+u.Name] {
				block(t.Name+" unique "+u.Name, "it uses dropped column "+c+"; list it under drops too")
			}
		}
		for _, fk := range t.ForeignKeys {
			if droppedObjects["foreign_key\x00"+t.Name+"\x00"+fk.Name] {
				continue
			}
			c := uses(fk.Columns)
			for _, rc := range fk.References.Columns {
				if droppedColumns[fk.References.Table+"."+rc] {
					c = rc
				}
			}
			if droppedTables[fk.References.Table] {
				block(t.Name+" foreign key "+fk.Name, "it references dropped table "+fk.References.Table+"; list it under drops too")
			} else if c != "" {
				block(t.Name+" foreign key "+fk.Name, "it uses dropped column "+c+"; list it under drops too")
			}
		}
		cols := columns(t)
		for _, ck := range t.Checks {
			if droppedObjects["check\x00"+t.Name+"\x00"+ck.Name] {
				continue
			}
			for _, c := range schema.CheckColumns(cols, ck.Expression) {
				if droppedColumns[t.Name+"."+c] {
					block(t.Name+" check "+ck.Name, "it uses dropped column "+c+"; list it under drops too")
					break
				}
			}
		}
	}
	return ops
}

// convertOperation returns the SQL and explanation for a listed type
// conversion, or a reason it is blocked.
func convertOperation(alterTable string, from, to schema.Column) (Operation, string) {
	if from.Identity != "" {
		return Operation{}, "converting an identity column is unsupported; its sequence keeps the old type"
	}
	if reason := conversionAllowed(from.Type, to.Type); reason != "" {
		return Operation{}, reason
	}
	sql := alterTable + " ALTER COLUMN " + quote(to.Name) + " TYPE " + to.Type
	explanation := "Convert " + to.Name + " from " + from.Type + " to " + to.Type + ". PostgreSQL rewrites the table and rebuilds its indexes under an exclusive lock; the migration fails and rolls back if any value does not convert."
	if schema.TextType(from.Type) && schema.TextType(to.Type) {
		// Assignment casts between text types fail on values that are too
		// long, where an explicit cast would cut them short.
		if n := schema.TypeLength(to.Type); n > 0 {
			explanation += fmt.Sprintf(" Values longer than %d characters make it fail.", n)
		}
	} else {
		sql += " USING " + quote(to.Name) + "::" + to.Type
	}
	if schema.IntegerType(to.Type) && !schema.IntegerType(from.Type) && (strings.HasPrefix(from.Type, "numeric") || from.Type == "real" || from.Type == "double precision") {
		explanation += " Fractions are rounded to the nearest integer."
	}
	if scale, ok := numericScale(to.Type); ok {
		if fromScale, bounded := numericScale(from.Type); !schema.IntegerType(from.Type) && (!bounded || fromScale > scale) {
			explanation += fmt.Sprintf(" Values are rounded to %d decimal places.", scale)
		}
	}
	return Operation{Kind: "convert_column_type", Subject: to.Name, SQL: sql + ";", Explanation: explanation}, ""
}

func numericScale(t string) (int, bool) {
	mod, ok := typeModifier(t, "numeric")
	if !ok || len(mod) != 2 {
		return 0, false
	}
	return mod[1], true
}

// conversionBlocker reports why a listed conversion cannot be planned:
// check constraints and foreign keys on the column would have to be
// rewritten with it.
func conversionBlocker(table, column string, working, target schema.Table, workingDoc, targetDoc schema.Document) string {
	for _, t := range []schema.Table{working, target} {
		cols := columns(t)
		for _, ck := range t.Checks {
			for _, c := range schema.CheckColumns(cols, ck.Expression) {
				if c == column {
					return "converting a column used by check constraint " + ck.Name + " is unsupported; drop the check first"
				}
			}
		}
	}
	for _, d := range []schema.Document{workingDoc, targetDoc} {
		for _, t := range d.Tables {
			for _, fk := range t.ForeignKeys {
				if (t.Name == table && contains(fk.Columns, column)) || (fk.References.Table == table && contains(fk.References.Columns, column)) {
					return "converting a column in foreign key " + fk.Name + " is unsupported"
				}
			}
		}
	}
	return ""
}

func sameSet(a, b []string) bool {
	x, y := append([]string{}, a...), append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	return strings.Join(x, "\x00") == strings.Join(y, "\x00")
}

// family groups the supported types for conversions.
func family(t string) string {
	switch {
	case schema.IntegerType(t):
		return "integer"
	case strings.HasPrefix(t, "numeric"):
		return "numeric"
	case t == "real" || t == "double precision":
		return "float"
	case schema.TextType(t):
		return "text"
	case t == "timestamp with time zone" || t == "timestamp without time zone":
		return "timestamp"
	}
	return t
}

// conversionAllowed returns why a conversion is not planned, or "". Only
// conversions whose result is fixed by the stored value are allowed; ones
// that depend on session settings (time zone, date style, bytea output),
// lose information without failing, or reinterpret values (integer to
// boolean, text to jsonb) are blocked.
func conversionAllowed(from, to string) string {
	f, t := family(from), family(to)
	switch {
	case schema.TypeLength(to) > 0 && f != "text":
		return "converting to a length-limited text type from another type family would cut values short silently; convert to text or varchar first"
	case t == "float" && !(from == "real" && to == "double precision"):
		return "converting to " + to + " can lose precision silently; only real to double precision is planned"
	case f == "date" || f == "timestamp":
		if from == "date" && to == "timestamp without time zone" {
			return ""
		}
		return "converting from " + from + " depends on the session time zone or date style; only date to timestamp without time zone is planned"
	case t == "date" || t == "timestamp":
		return "converting to " + to + " depends on the session time zone or date style"
	case f == "bytea" || t == "bytea":
		return "converting bytea depends on the session's bytea settings"
	case t == "jsonb" || f == "jsonb" && t != "text":
		return "converting to or from jsonb other than jsonb to text is unsupported; PostgreSQL normalizes JSON text"
	case t == "boolean" && f != "text":
		return "converting " + from + " to boolean turns every nonzero value into true"
	case f == "boolean" && t == "integer" && to != "integer":
		return "boolean converts to integer only"
	case t == "uuid" && f != "text":
		return "only text converts to uuid"
	}
	switch f + ">" + t {
	case "integer>integer", "integer>numeric", "numeric>integer", "numeric>numeric", "float>integer", "float>numeric", "float>float",
		"text>text", "text>integer", "text>numeric", "text>boolean", "text>uuid",
		"integer>text", "numeric>text", "boolean>text", "uuid>text", "jsonb>text", "boolean>integer":
		return ""
	}
	return "converting " + from + " to " + to + " is unsupported"
}
