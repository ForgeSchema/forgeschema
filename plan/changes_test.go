// Copyright 2026 ForgeSchema
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ForgeSchema/forgeschema/schema"
)

const stagedBase = `version: 1
schema: public
tables:
- name: customers
  columns:
  - name: id
    type: bigint
  - name: legacy_code
    type: text
    nullable: true
  - name: amount
    type: text
    default: "'0'"
  primary_key: [id]
  indexes:
  - name: customers_legacy_idx
    columns: [legacy_code]
  checks:
  - name: customers_legacy_check
    expression: legacy_code <> ''
- name: notes
  columns:
  - name: id
    type: bigint
  - name: customer_id
    type: bigint
  primary_key: [id]
  foreign_keys:
  - name: notes_customer_fkey
    columns: [customer_id]
    references: {table: customers, columns: [id]}
- name: note_tags
  columns:
  - name: note_id
    type: bigint
  foreign_keys:
  - name: note_tags_note_fkey
    columns: [note_id]
    references: {table: notes, columns: [id]}
`

func stagedDocs(t *testing.T) (schema.Document, schema.Document) {
	t.Helper()
	src := doc(t, stagedBase)
	dst := doc(t, stagedBase)
	dst.Tables = []schema.Table{dst.Tables[0]}
	c := &dst.Tables[0]
	c.Columns = []schema.Column{c.Columns[0], c.Columns[1]} // amount, id
	c.Indexes, c.Checks = nil, nil
	return src, dst
}

func opList(p Plan) []string {
	var out []string
	for _, op := range p.Operations {
		out = append(out, op.Kind+" "+op.Table+" "+op.Subject)
	}
	return out
}

func TestDropsAreExplicit(t *testing.T) {
	src, dst := stagedDocs(t)
	p, err := Build(src, dst, nil)
	if err != nil || p.Status != "blocked" || len(p.Operations) != 0 {
		t.Fatalf("unlisted drops: %+v %v", p, err)
	}
	drops := []Drop{
		{Table: "note_tags"}, {Table: "notes"},
		{Table: "customers", Column: "legacy_code"}, {Table: "customers", Index: "customers_legacy_idx"}, {Table: "customers", Check: "customers_legacy_check"},
	}
	p, err = BuildWith(src, dst, nil, Options{Drops: drops})
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	want := []string{
		"drop_foreign_key note_tags note_tags_note_fkey",
		"drop_check customers customers_legacy_check",
		"drop_index customers customers_legacy_idx",
		"drop_column customers legacy_code",
		"drop_table note_tags ",
		"drop_table notes ",
	}
	if got := opList(p); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("operations:\n%s", strings.Join(got, "\n"))
	}
	summary, err := p.Summary()
	if err != nil || !strings.Contains(summary, "This plan permanently deletes data: column customers.legacy_code, table note_tags, table notes.") {
		t.Fatalf("%s %v", summary, err)
	}
	// Dropping notes also changes customers, which its foreign key references.
	if exclusive, _ := p.LockedTables(); strings.Join(exclusive, ",") != "customers,note_tags,notes" {
		t.Fatalf("locks %v", exclusive)
	}
	b, err := p.JSON()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(bytes.NewReader(b))
	if err != nil || loaded.Validate() != nil {
		t.Fatalf("saved plan with drops does not load: %v", err)
	}
	// Removing the drop intent from the artifact makes it fail validation.
	loaded.Drops = loaded.Drops[1:]
	if loaded.Validate() == nil {
		t.Fatal("tampered drops accepted")
	}

	// A dropped column's index must be listed too.
	p, err = BuildWith(src, dst, nil, Options{Drops: []Drop{{Table: "note_tags"}, {Table: "notes"}, {Table: "customers", Column: "legacy_code"}, {Table: "customers", Check: "customers_legacy_check"}}})
	if err != nil || p.Status != "blocked" || !strings.Contains(p.Blocked[0].Reason+p.Blocked[len(p.Blocked)-1].Reason, "list it under drops too") {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	// So must a foreign key that references a dropped table.
	p, _ = BuildWith(src, dst, nil, Options{Drops: []Drop{{Table: "notes"}, {Table: "customers", Column: "legacy_code"}, {Table: "customers", Index: "customers_legacy_idx"}, {Table: "customers", Check: "customers_legacy_check"}}})
	if p.Status != "blocked" {
		t.Fatal("dangling foreign key not blocked")
	}

	for name, bad := range map[string][]Drop{
		"still in target":  {{Table: "customers", Column: "amount"}},
		"not in source":    {{Table: "customers", Column: "nope"}},
		"two objects":      {{Table: "customers", Column: "legacy_code", Index: "customers_legacy_idx"}},
		"table kept":       {{Table: "customers"}},
		"twice":            {{Table: "notes"}, {Table: "notes"}},
		"on dropped table": {{Table: "notes", ForeignKey: "notes_customer_fkey"}},
	} {
		if _, err := BuildWith(src, dst, nil, Options{Drops: bad}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRenameIntoDroppedColumn(t *testing.T) {
	src := doc(t, base)
	src.Tables[0].Columns = append(src.Tables[0].Columns, schema.Column{Name: "email_new", Type: "varchar(320)", Nullable: true})
	dst := doc(t, base)
	dst.Tables[0].Columns[1] = schema.Column{Name: "email", Type: "varchar(320)", Nullable: true}
	renames := []Rename{{Table: "customers", From: "email_new", To: "email"}}
	if _, err := Build(src, dst, renames); err == nil {
		t.Fatal("rename onto an existing column accepted")
	}
	p, err := BuildWith(src, dst, renames, Options{Drops: []Drop{{Table: "customers", Column: "email"}, {Table: "customers", Index: "customers_email_idx"}}})
	if err == nil {
		t.Fatal("dropping an index the target keeps accepted")
	}
	dst.Tables[0].Indexes = nil
	p, err = BuildWith(src, dst, renames, Options{Drops: []Drop{{Table: "customers", Column: "email"}, {Table: "customers", Index: "customers_email_idx"}}})
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	if got := strings.Join(opList(p), "\n"); got != "drop_index customers customers_email_idx\ndrop_column customers email\nrename_column customers email_new" {
		t.Fatalf("operations:\n%s", got)
	}
}

func TestConversions(t *testing.T) {
	src := doc(t, stagedBase)
	dst := doc(t, stagedBase)
	amount := &dst.Tables[0].Columns[0]
	if amount.Name != "amount" {
		t.Fatal("fixture order")
	}
	amount.Type, amount.Default = "integer", "0"
	if p, _ := Build(src, dst, nil); p.Status != "blocked" {
		t.Fatal("unlisted conversion planned")
	}
	p, err := BuildWith(src, dst, nil, Options{Conversions: []Conversion{{Table: "customers", Column: "amount"}}})
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	var sqls []string
	for _, op := range p.Operations {
		sqls = append(sqls, op.SQL)
	}
	want := `ALTER TABLE "public"."customers" ALTER COLUMN "amount" DROP DEFAULT;
ALTER TABLE "public"."customers" ALTER COLUMN "amount" TYPE integer USING "amount"::integer;
ALTER TABLE "public"."customers" ALTER COLUMN "amount" SET DEFAULT 0;`
	if strings.Join(sqls, "\n") != want {
		t.Fatalf("sql:\n%s", strings.Join(sqls, "\n"))
	}
	conv := []Conversion{{Table: "customers", Column: "amount"}}
	amount.Type, amount.Default = "varchar(5)", ""
	p, _ = BuildWith(src, dst, nil, Options{Conversions: conv})
	if p.Status != "ready" || strings.Contains(p.Operations[1].SQL, "USING") || !strings.Contains(p.Operations[1].Explanation, "longer than 5") {
		t.Fatalf("text to varchar: %+v", p.Operations)
	}
	amount.Type = "numeric(10,2)"
	p, _ = BuildWith(src, dst, nil, Options{Conversions: conv})
	if p.Status != "ready" || !strings.Contains(p.Operations[1].Explanation, "rounded to 2 decimal places") {
		t.Fatalf("text to numeric: %+v %+v", p.Operations, p.Blocked)
	}
	// Columns in checks and foreign keys are not converted.
	legacy := &dst.Tables[0].Columns[2]
	amount.Type, amount.Default = "text", "'0'"
	legacy.Type = "integer"
	dst.Tables[0].Checks = nil
	dst.Tables[0].Indexes = nil
	p, err = BuildWith(src, dst, nil, Options{Conversions: []Conversion{{Table: "customers", Column: "legacy_code"}}, Drops: []Drop{{Table: "customers", Check: "customers_legacy_check"}, {Table: "customers", Index: "customers_legacy_idx"}}})
	// Dropping the check in the same plan clears the way: it goes first.
	if err != nil || p.Status != "ready" || p.Operations[0].Kind != "drop_check" {
		t.Fatalf("check dropped first: %+v %v", p.Blocked, err)
	}
	legacy.Type = "text"
	amount.Type = "varchar(5)"
	dst.Tables[0].Checks = []schema.Check{{Name: "customers_amount_check", Expression: "amount <> ''"}}
	src.Tables[0].Checks = append(src.Tables[0].Checks, schema.Check{Name: "customers_amount_check", Expression: "amount <> ''"})
	p, err = BuildWith(src, dst, nil, Options{Conversions: conv, Drops: []Drop{{Table: "customers", Check: "customers_legacy_check"}, {Table: "customers", Index: "customers_legacy_idx"}}})
	if err != nil || p.Status != "blocked" || !strings.Contains(p.Blocked[0].Reason, "check constraint customers_amount_check") {
		t.Fatalf("check column: %+v %v", p.Blocked, err)
	}
	dst = doc(t, stagedBase)
	dst.Tables[0].Columns[1].Type = "integer"
	p, err = BuildWith(src, dst, nil, Options{Conversions: []Conversion{{Table: "customers", Column: "id"}}})
	if err != nil || p.Status != "blocked" || !strings.Contains(p.Blocked[len(p.Blocked)-1].Reason, "foreign key notes_customer_fkey") {
		t.Fatalf("foreign key column: %+v %v", p.Blocked, err)
	}
	for _, bad := range [][]Conversion{{{Table: "customers", Column: "legacy_code"}}, {{Table: "nope", Column: "id"}}, {{Table: "customers", Column: "amount"}, {Table: "customers", Column: "amount"}}} {
		if _, err := BuildWith(src, dst, nil, Options{Conversions: bad}); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestLoadChanges(t *testing.T) {
	c, err := LoadChanges(strings.NewReader("version: 1\ndrops:\n- table: notes\n- table: customers\n  column: legacy_code\nconversions:\n- table: customers\n  column: amount\n"))
	if err != nil || len(c.Drops) != 2 || len(c.Conversions) != 1 || c.Renames == nil {
		t.Fatalf("%+v %v", c, err)
	}
	for _, bad := range []string{"version: 1\n", "version: 2\ncolumns: []\n", "version: 1\ndrops:\n- table: t\n  view: v\n"} {
		if _, err := LoadChanges(strings.NewReader(bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := LoadRenames(strings.NewReader("version: 1\ncolumns: []\ndrops:\n- table: t\n")); err == nil {
		t.Error("rename loader ignored drops")
	}
}
