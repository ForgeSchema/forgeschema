package plan

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/ForgeSchema/forgeschema/schema"
)

func kinds(p Plan) string {
	var out []string
	for _, op := range p.Operations {
		out = append(out, op.Kind+":"+op.Table+"."+op.Subject)
	}
	return strings.Join(out, " ")
}

func ready(t *testing.T, src, dst schema.Document, renames []Rename) Plan {
	t.Helper()
	p, err := Build(src, dst, renames)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "ready" {
		t.Fatalf("blocked: %+v", p.Blocked)
	}
	if err = p.Validate(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestShopFromEmptyOrdersOperations(t *testing.T) {
	f, err := os.Open("../examples/shop.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dst, err := schema.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	p := ready(t, doc(t, "version: 1\nschema: public\ntables: []\n"), dst, nil)
	want := "create_table:customers. create_table:order_lines. create_table:orders. create_index:order_lines.order_lines_sku_idx create_index:orders.orders_customer_idx add_foreign_key:order_lines.order_lines_order_fkey add_foreign_key:orders.orders_customer_fkey"
	if got := kinds(p); got != want {
		t.Fatalf("order\n got %s\nwant %s", got, want)
	}
	sql := p.Operations[0].SQL
	for _, part := range []string{`"id" bigint GENERATED ALWAYS AS IDENTITY NOT NULL`, `"status" character varying(20) DEFAULT 'active' NOT NULL`, `"created_at" timestamp with time zone DEFAULT now() NOT NULL`, `CONSTRAINT "customers_email_key" UNIQUE ("email")`} {
		if !strings.Contains(sql, part) {
			t.Errorf("customers SQL lacks %s: %s", part, sql)
		}
	}
	if !strings.Contains(p.Operations[2].SQL, `DEFAULT CURRENT_DATE NOT NULL`) || !strings.Contains(p.Operations[6].SQL, `REFERENCES "public"."customers" ("id") ON DELETE RESTRICT;`) {
		t.Fatalf("defaults/foreign keys not rendered: %s / %s", p.Operations[2].SQL, p.Operations[6].SQL)
	}
	if ex, ref := p.LockedTables(); len(ex)+len(ref) != 0 {
		t.Fatalf("new tables need no locks: %v %v", ex, ref)
	}
}

const two = `version: 1
schema: public
tables:
- name: accounts
  columns:
  - name: id
    type: bigint
  - name: email
    type: varchar(50)
    nullable: true
  - name: note
    type: text
  - name: amount
    type: numeric(8,2)
    nullable: true
  primary_key: [id]
- name: audit
  columns:
  - name: id
    type: bigint
  primary_key: [id]
`

func TestAlterColumnOperations(t *testing.T) {
	src := doc(t, two)
	dst, _ := schema.Normalize(src)
	cols := dst.Tables[0].Columns // amount, email, id, note
	cols[0].Type = "numeric(12,2)"
	cols[1].Type, cols[1].Nullable = "varchar(100)", false
	cols[3].Nullable, cols[3].Default = true, "''"
	dst.Tables[0].Columns = append(cols,
		schema.Column{Name: "status", Type: "text", Default: "'new'"},
		schema.Column{Name: "created_at", Type: "timestamptz", Default: "now()"},
		schema.Column{Name: "token", Type: "uuid", Default: "gen_random_uuid()"},
	)
	p := ready(t, src, dst, nil)
	want := "add_column:accounts.created_at add_column:accounts.status add_column:accounts.token alter_column_type:accounts.amount alter_column_type:accounts.email set_not_null:accounts.email drop_not_null:accounts.note set_default:accounts.note"
	if got := kinds(p); got != want {
		t.Fatalf("operations\n got %s\nwant %s", got, want)
	}
	summary, _ := p.Summary()
	for _, s := range []string{`ADD COLUMN "status" text DEFAULT 'new' NOT NULL;`, "rewrites the whole table", "receive the value at migration time", `TYPE character varying(100);`, "Locks: ACCESS EXCLUSIVE on accounts"} {
		if !strings.Contains(summary, s) {
			t.Errorf("summary lacks %q:\n%s", s, summary)
		}
	}
	// Reverse direction: dropping the default is planned, narrowing is not.
	back, _ := schema.Normalize(p.Target)
	for i := range back.Tables[0].Columns {
		if back.Tables[0].Columns[i].Name == "note" {
			back.Tables[0].Columns[i].Default = ""
		}
	}
	if got := kinds(ready(t, p.Target, back, nil)); got != "drop_default:accounts.note" {
		t.Fatalf("missing drop_default: %s", got)
	}
}

func TestStillBlocked(t *testing.T) {
	src := doc(t, two)
	cases := map[string]func(*schema.Document){
		"narrow-varchar": func(d *schema.Document) { d.Tables[0].Columns[1].Type = "varchar(10)" },
		"numeric-scale":  func(d *schema.Document) { d.Tables[0].Columns[0].Type = "numeric(12,4)" },
		"int-to-bigint":  func(d *schema.Document) { d.Tables[1].Columns[0].Type = "integer" },
		"identity-added": func(d *schema.Document) { d.Tables[1].Columns[0].Identity = "always" },
		"required-no-default": func(d *schema.Document) {
			d.Tables[1].Columns = append(d.Tables[1].Columns, schema.Column{Name: "x", Type: "text"})
		},
	}
	for name, change := range cases {
		dst, _ := schema.Normalize(src)
		change(&dst)
		p, err := Build(src, dst, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p.Status != "blocked" || len(p.Operations) != 0 {
			t.Errorf("%s: not blocked: %s", name, kinds(p))
		}
	}
}

const related = `version: 1
schema: public
tables:
- name: parents
  columns:
  - name: id
    type: bigint
  - name: code
    type: text
  primary_key: [id]
- name: children
  columns:
  - name: id
    type: bigint
    identity: by_default
  - name: parent_id
    type: bigint
  primary_key: [id]
  foreign_keys:
  - name: children_parent_fkey
    columns: [parent_id]
    references:
      table: parents
      columns: [id]
`

func TestConstraintOperationsAndLocks(t *testing.T) {
	src := doc(t, related)
	dst, _ := schema.Normalize(src)
	dst.Tables[1].Unique = []schema.Unique{{Name: "parents_code_key", Columns: []string{"code"}}}
	dst.Tables[1].Indexes = []schema.Index{{Name: "parents_code_idx", Columns: []string{"code", "id"}, Unique: true}}
	dst.Tables[0].Columns = append(dst.Tables[0].Columns, schema.Column{Name: "parent_code", Type: "text", Nullable: true})
	dst.Tables[0].ForeignKeys = append(dst.Tables[0].ForeignKeys, schema.ForeignKey{Name: "children_code_fkey", Columns: []string{"parent_code"}, References: schema.Reference{Table: "parents", Columns: []string{"code"}}, OnDelete: "set_null"})
	p := ready(t, src, dst, nil)
	want := "add_column:children.parent_code add_unique:parents.parents_code_key create_index:parents.parents_code_idx add_foreign_key:children.children_code_fkey"
	if got := kinds(p); got != want {
		t.Fatalf("operations\n got %s\nwant %s", got, want)
	}
	if !strings.Contains(p.Operations[2].SQL, "CREATE UNIQUE INDEX") || !strings.Contains(p.Operations[3].SQL, "ON DELETE SET NULL;") {
		t.Fatalf("SQL: %s / %s", p.Operations[2].SQL, p.Operations[3].SQL)
	}
	ex, ref := p.LockedTables()
	if strings.Join(ex, ",") != "children,parents" || len(ref) != 0 {
		t.Fatalf("locks %v %v", ex, ref)
	}
	// A foreign key alone locks the referenced table in the weaker mode.
	only, _ := schema.Normalize(src)
	only.Tables[1].Unique = []schema.Unique{{Name: "parents_code_key", Columns: []string{"code"}}}
	mid := ready(t, src, only, nil)
	step2, _ := schema.Normalize(only)
	step2.Tables[0].Columns = append(step2.Tables[0].Columns, schema.Column{Name: "parent_code", Type: "text", Nullable: true})
	step2.Tables[0].ForeignKeys = append(step2.Tables[0].ForeignKeys, schema.ForeignKey{Name: "children_code_fkey", Columns: []string{"parent_code"}, References: schema.Reference{Table: "parents", Columns: []string{"code"}}})
	p2 := ready(t, mid.Target, step2, nil)
	ex, ref = p2.LockedTables()
	if strings.Join(ex, ",") != "children" || strings.Join(ref, ",") != "parents" {
		t.Fatalf("fk locks %v %v", ex, ref)
	}
	blocked := map[string]func(*schema.Document){
		"drop-fk":   func(d *schema.Document) { d.Tables[0].ForeignKeys = nil },
		"change-fk": func(d *schema.Document) { d.Tables[0].ForeignKeys[0].OnDelete = "cascade" },
		"drop-unique": func(d *schema.Document) {
			d.Tables[1].Unique = nil
			d.Tables[0].ForeignKeys = d.Tables[0].ForeignKeys[1:] // children_code_fkey needs it
		},
		"index-unique-flip": func(d *schema.Document) {
			d.Tables[1].Indexes = []schema.Index{{Name: "parents_code_idx", Columns: []string{"code", "id"}}}
		},
	}
	for name, change := range blocked {
		d, _ := schema.Normalize(dst)
		change(&d)
		b, err := Build(dst, d, nil)
		if err != nil || b.Status != "blocked" {
			t.Errorf("%s: %+v %v", name, b.Blocked, err)
		}
	}
}

func TestRenamePropagatesToForeignKeys(t *testing.T) {
	src := doc(t, related)
	dst, _ := schema.Normalize(src)
	parents := &dst.Tables[1]
	for i := range parents.Columns {
		if parents.Columns[i].Name == "id" {
			parents.Columns[i].Name = "parent_key"
		}
	}
	parents.PrimaryKey = []string{"parent_key"}
	dst.Tables[0].ForeignKeys[0].References.Columns = []string{"parent_key"}
	p := ready(t, src, dst, []Rename{{"parents", "id", "parent_key"}})
	if kinds(p) != "rename_column:parents.id" {
		t.Fatalf("rename should be the only operation: %s", kinds(p))
	}
	// Identity columns keep their sequence name, so renaming one is blocked.
	d2, _ := schema.Normalize(src)
	for i := range d2.Tables[0].Columns {
		if d2.Tables[0].Columns[i].Name == "id" {
			d2.Tables[0].Columns[i].Name = "child_id"
		}
	}
	d2.Tables[0].PrimaryKey = []string{"child_id"}
	b, err := Build(src, d2, []Rename{{"children", "id", "child_id"}})
	if err != nil || b.Status != "blocked" || !strings.Contains(b.Blocked[0].Reason, "identity") {
		t.Fatalf("identity rename: %+v %v", b, err)
	}
}

// Saved 0.1 plans must keep loading: their explanations and SQL are rebuilt
// and compared byte for byte.
func TestSavedPlansStillValidate(t *testing.T) {
	for _, path := range []string{"../examples/customer-plan.json", "../examples/walkthrough/postgres-16.10/reviewed-plan.json", "../examples/walkthrough/postgres-17.6/reviewed-plan.json"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = Load(bytes.NewReader(b)); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestTypeChangeLocksForeignKeyPartners(t *testing.T) {
	src := doc(t, `version: 1
schema: public
tables:
- name: p
  columns:
  - name: code
    type: varchar(5)
  primary_key: [code]
- name: c
  columns:
  - name: code
    type: varchar(5)
  foreign_keys:
  - name: c_code_fkey
    columns: [code]
    references:
      table: p
      columns: [code]
- name: other
  columns:
  - name: id
    type: integer
`)
	for _, table := range []string{"p", "c"} {
		dst, _ := schema.Normalize(src)
		for ti := range dst.Tables {
			if dst.Tables[ti].Name == table {
				dst.Tables[ti].Columns[0].Type = "varchar(10)"
			}
		}
		p := ready(t, src, dst, nil)
		ex, ref := p.LockedTables()
		if strings.Join(ex, ",") != "c,p" || len(ref) != 0 {
			t.Errorf("widening %s.code: locks %v %v", table, ex, ref)
		}
	}
}
