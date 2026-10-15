package plan

import (
	"encoding/json"
	"github.com/ForgeSchema/forgeschema/schema"
	"strings"
	"testing"
)

func doc(t *testing.T, input string) schema.Document {
	t.Helper()
	d, err := schema.Load(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

const base = `version: 1
schema: public
tables:
- name: customers
  columns:
  - name: id
    type: bigint
  - name: customer_name
    type: text
  - name: email
    type: text
    nullable: true
  primary_key: [id]
  indexes:
  - name: customers_email_idx
    columns: [email]
`

func TestEquivalentSchemas(t *testing.T) {
	a := doc(t, base)
	b := doc(t, strings.Replace(base, "type: bigint", "type: int8", 1))
	b.Tables[0].Columns[0], b.Tables[0].Columns[2] = b.Tables[0].Columns[2], b.Tables[0].Columns[0]
	p, err := Build(a, b, nil)
	if err != nil || p.Status != "ready" || len(p.Operations) != 0 || p.SourceFingerprint != p.TargetFingerprint {
		t.Fatalf("unexpected plan %+v %v", p, err)
	}
	if _, err = p.JSON(); err != nil {
		t.Fatal(err)
	}
}
func TestRenameAndAdd(t *testing.T) {
	src := doc(t, base)
	dst := doc(t, strings.Replace(base, "customer_name", "full_name", 1))
	dst.Tables[0].Columns = append(dst.Tables[0].Columns, schema.Column{Name: "notes", Type: "text", Nullable: true})
	dst.Tables[0].Indexes = append(dst.Tables[0].Indexes, schema.Index{Name: "customers_notes_idx", Columns: []string{"notes"}})
	hints := []Rename{{Table: "customers", From: "customer_name", To: "full_name"}}
	p, err := Build(src, dst, hints)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "ready" || len(p.Operations) != 3 {
		t.Fatalf("unexpected plan %+v", p)
	}
	want := []string{`ALTER TABLE "public"."customers" RENAME COLUMN "customer_name" TO "full_name";`, `ALTER TABLE "public"."customers" ADD COLUMN "notes" text NULL;`, `CREATE INDEX "customers_notes_idx" ON "public"."customers" ("notes");`}
	for i, op := range p.Operations {
		if op.SQL != want[i] {
			t.Fatalf("unexpected SQL %s", op.SQL)
		}
	}
	if src.Tables[0].Columns[0].Name != "customer_name" {
		t.Fatal("mutated caller")
	}
	b, err := p.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip Plan
	if err = json.Unmarshal(b, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if err = roundtrip.Validate(); err != nil {
		t.Fatal(err)
	}
	q, err := Build(src, dst, hints)
	if err != nil {
		t.Fatal(err)
	}
	qb, _ := q.JSON()
	if string(b) != string(qb) {
		t.Fatal("nondeterministic plan")
	}
	summary, err := p.Summary()
	if err != nil || !strings.Contains(summary, "Review only") {
		t.Fatal("missing review output")
	}
}
func TestRenameKeyAndIndexReferences(t *testing.T) {
	src := doc(t, base)
	src.Tables[0].Indexes = append(src.Tables[0].Indexes, schema.Index{Name: "customers_name_idx", Columns: []string{"customer_name", "id"}})
	dst := doc(t, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(base, "customer_name", "full_name"), "name: id", "name: customer_id"), "[id]", "[customer_id]"))
	for i := range dst.Tables[0].Columns {
		if dst.Tables[0].Columns[i].Name == "id" {
			dst.Tables[0].Columns[i].Name = "customer_id"
		}
	}
	dst.Tables[0].Indexes = append(dst.Tables[0].Indexes, schema.Index{Name: "customers_name_idx", Columns: []string{"full_name", "customer_id"}})
	p, err := Build(src, dst, []Rename{{"customers", "id", "customer_id"}, {"customers", "customer_name", "full_name"}})
	if err != nil || p.Status != "ready" || len(p.Operations) != 2 {
		t.Fatalf("key/index identity lost: %+v %v", p, err)
	}
}
func TestCreateQuotedTable(t *testing.T) {
	src := schema.Document{Version: 1, Schema: "public", Tables: []schema.Table{}}
	dst := schema.Document{Version: 1, Schema: "public", Tables: []schema.Table{{Name: "select", Columns: []schema.Column{{Name: "from", Type: "int4"}}, PrimaryKey: []string{"from"}, Indexes: []schema.Index{{Name: "select_from_idx", Columns: []string{"from"}}}}}}
	p, err := Build(src, dst, nil)
	if err != nil || p.Status != "ready" || len(p.Operations) != 2 {
		t.Fatalf("%+v %v", p, err)
	}
	if p.Operations[0].SQL != `CREATE TABLE "public"."select" ("from" integer NOT NULL, CONSTRAINT "select_pkey" PRIMARY KEY ("from"));` {
		t.Fatal(p.Operations[0].SQL)
	}
}
func TestBlockedChangesHaveNoSQL(t *testing.T) {
	cases := map[string]func(*schema.Document){
		"drop-table":  func(d *schema.Document) { d.Tables = []schema.Table{} },
		"drop-column": func(d *schema.Document) { d.Tables[0].Columns = d.Tables[0].Columns[1:] },
		"required-column": func(d *schema.Document) {
			d.Tables[0].Columns = append(d.Tables[0].Columns, schema.Column{Name: "required", Type: "text"})
		},
		"type-change":     func(d *schema.Document) { d.Tables[0].Columns[0].Type = "integer" },
		"nullability":     func(d *schema.Document) { d.Tables[0].Columns[0].Nullable = true },
		"pk-change":       func(d *schema.Document) { d.Tables[0].PrimaryKey = nil },
		"index-drop":      func(d *schema.Document) { d.Tables[0].Indexes = nil },
		"index-change":    func(d *schema.Document) { d.Tables[0].Indexes[0].Columns = []string{"id"} },
		"implicit-rename": func(d *schema.Document) { d.Tables[0].Columns[0].Name = "full_name" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			src := doc(t, base)
			dst := doc(t, base)
			change(&dst)
			// A separate supported addition must also disappear from a blocked plan.
			dst.Tables = append(dst.Tables, schema.Table{Name: "new_table", Columns: []schema.Column{{Name: "id", Type: "integer"}}})
			p, err := Build(src, dst, nil)
			if err != nil {
				t.Fatal(err)
			}
			if p.Status != "blocked" || len(p.Operations) != 0 || len(p.Blocked) == 0 {
				t.Fatalf("partial unsafe plan: %+v", p)
			}
			if err = p.Validate(); err != nil {
				t.Fatal(err)
			}
			summary, _ := p.Summary()
			if strings.Contains(summary, "CREATE TABLE") {
				t.Fatal("blocked summary leaked SQL")
			}
		})
	}
}
func TestInvalidRenameIntent(t *testing.T) {
	src := doc(t, base)
	dst := doc(t, strings.Replace(base, "customer_name", "full_name", 1))
	cases := [][]Rename{
		{{"missing", "customer_name", "full_name"}}, {{"customers", "missing", "full_name"}}, {{"customers", "customer_name", "missing"}}, {{"customers", "id", "id"}}, {{"customers", "customer_name", "full_name"}, {"customers", "customer_name", "full_name"}}, {{"customers", "id", "full_name"}, {"customers", "customer_name", "full_name"}}, {{"customers", "customer_name", "email"}},
	}
	for _, r := range cases {
		if _, err := Build(src, dst, r); err == nil {
			t.Fatalf("accepted invalid mapping %+v", r)
		}
	}
	// Source-name reuse is ambiguous and deliberately rejected.
	dst.Tables[0].Columns = append(dst.Tables[0].Columns, schema.Column{Name: "customer_name", Type: "text", Nullable: true})
	if _, err := Build(src, dst, []Rename{{"customers", "customer_name", "full_name"}}); err == nil {
		t.Fatal("accepted source-name reuse")
	}
}
func TestTamperingRejected(t *testing.T) {
	src := doc(t, base)
	dst := doc(t, strings.Replace(base, "customer_name", "full_name", 1))
	original, err := Build(src, dst, []Rename{{"customers", "customer_name", "full_name"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := original.JSON()
	cases := []func(*Plan){func(p *Plan) { p.FormatVersion = 2 }, func(p *Plan) { p.Operations[0].SQL = "DROP TABLE customers;" }, func(p *Plan) { p.Operations[0].Kind = "drop_table" }, func(p *Plan) { p.Operations[0].Explanation = "different" }, func(p *Plan) { p.SourceFingerprint = "bad" }, func(p *Plan) { p.TargetFingerprint = "bad" }, func(p *Plan) { p.Status = "blocked" }, func(p *Plan) { p.Operations = nil }, func(p *Plan) { p.Target.Tables[0].Columns[0].Type = "integer" }}
	for _, change := range cases {
		var p Plan
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		change(&p)
		if err := p.Validate(); err == nil {
			t.Fatal("accepted tampered artifact")
		}
	}
}
func TestStrictRenames(t *testing.T) {
	valid := "version: 1\ncolumns:\n- table: customers\n  from: customer_name\n  to: full_name\n"
	if _, err := LoadRenames(strings.NewReader(valid)); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{strings.Replace(valid, "version: 1", "version: 2", 1), valid + "extra: 1\n", strings.Replace(valid, "from: customer_name", "from: 1", 1), strings.Replace(valid, "to: full_name", "to: &a full_name", 1), valid + "---\n", "version: 1\n", "version: 1\ncolumns: null\n"} {
		if _, err := LoadRenames(strings.NewReader(v)); err == nil {
			t.Fatalf("accepted invalid renames %s", v)
		}
	}
}
func TestDifferentSchemasAndInvalidInput(t *testing.T) {
	src := doc(t, base)
	dst := doc(t, base)
	dst.Schema = "other"
	if _, err := Build(src, dst, nil); err == nil {
		t.Fatal("accepted schema move")
	}
	src.Version = 0
	if _, err := Build(src, dst, nil); err == nil {
		t.Fatal("accepted invalid source")
	}
}
