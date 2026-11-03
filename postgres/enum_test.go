package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
)

func enumDocument(t *testing.T, name, y string) schema.Document {
	t.Helper()
	d, err := schema.Load(strings.NewReader("version: 1\nschema: " + name + "\n" + y))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

const enumTables = `enums:
- name: status
  values: [active, "it's closed"]
tables:
- name: orders
  columns:
  - name: id
    type: bigint
    identity: always
  - name: state
    type: status
    default: "'active'"
  - name: tags
    type: text[]
    default: "'{}'"
  - name: scores
    type: integer[]
    nullable: true
  - name: history
    type: status[]
    nullable: true
  - name: codes
    type: varchar(12)[]
    nullable: true
  primary_key: [id]
  indexes:
  - name: orders_state
    columns: [state]
- name: states
  columns:
  - name: state
    type: status
  primary_key: [state]
- name: notes
  columns:
  - name: id
    type: bigint
  - name: state
    type: status
  primary_key: [id]
  foreign_keys:
  - name: notes_state_fkey
    columns: [state]
    references: {table: states, columns: [state]}
`

// applyPlan plans from the live schema to want, applies it and verifies.
func applyPlan(t *testing.T, name string, want schema.Document) plan.Plan {
	t.Helper()
	db := database(t)
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.Build(src, want, nil)
	if err != nil || p.Status != "ready" {
		t.Fatalf("plan: %+v %v", p.Blocked, err)
	}
	if _, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatalf("apply: %v\n%s", err, sqlOf(p))
	}
	if v, err := postgres.Verify(context.Background(), db, want); err != nil || v.Status != "match" {
		t.Fatalf("verify: %+v %v", v, err)
	}
	return p
}

func sqlOf(p plan.Plan) string {
	var b strings.Builder
	for _, op := range p.Operations {
		b.WriteString(op.SQL + "\n")
	}
	return b.String()
}

func TestEnumsAndArraysRoundTrip(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	want := enumDocument(t, name, enumTables)
	p := applyPlan(t, name, want)
	if p.Operations[0].Kind != "create_enum" {
		t.Fatalf("first operation %s", p.Operations[0].Kind)
	}
	// Inspection gives back exactly the document.
	got, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := got.CanonicalJSON()
	b, _ := want.CanonicalJSON()
	if string(a) != string(b) {
		t.Fatalf("inspect differs\n got  %s\n want %s", a, b)
	}
	// The created objects behave as declared.
	exec(t, db, "INSERT INTO "+name+".orders(scores, history, codes) VALUES ('{1,2}', '{active}', '{abc}')")
	var state, tags string
	if err := db.QueryRow("SELECT state::text, tags::text FROM "+name+".orders").Scan(&state, &tags); err != nil {
		t.Fatal(err)
	}
	if state != "active" || tags != "{}" {
		t.Fatalf("defaults %q %q", state, tags)
	}
	if _, err := db.Exec("INSERT INTO " + name + ".orders(state) VALUES ('gone')"); err == nil {
		t.Fatal("enum accepted a value it does not have")
	}

	// Values added at the start, the middle and the end, and a new array
	// column on the populated table.
	next := enumDocument(t, name, strings.Replace(strings.Replace(enumTables,
		`values: [active, "it's closed"]`, `values: [new, active, "on hold", "it's closed", archived]`, 1),
		"  primary_key: [id]\n  indexes:", "  - name: labels\n    type: status[]\n    nullable: true\n  primary_key: [id]\n  indexes:", 1))
	p = applyPlan(t, name, next)
	kinds := []string{}
	for _, op := range p.Operations {
		kinds = append(kinds, op.Kind)
	}
	if strings.Join(kinds, ",") != "add_enum_value,add_enum_value,add_enum_value,add_column" {
		t.Fatalf("operations %v\n%s", kinds, sqlOf(p))
	}
	var order string
	if err := db.QueryRow("SELECT string_agg(e::text, ',' ORDER BY e) FROM unnest(enum_range(NULL::" + name + ".status)) e").Scan(&order); err != nil {
		t.Fatal(err)
	}
	if order != "new,active,on hold,it's closed,archived" {
		t.Fatalf("enum order %q", order)
	}
}

func TestEnumChangesThatAreBlocked(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	base := enumDocument(t, name, enumTables)
	applyPlan(t, name, base)
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"removed value":   strings.Replace(enumTables, `values: [active, "it's closed"]`, `values: [active]`, 1),
		"reordered":       strings.Replace(enumTables, `values: [active, "it's closed"]`, `values: ["it's closed", active]`, 1),
		"new value used":  strings.Replace(strings.Replace(enumTables, `values: [active, "it's closed"]`, `values: [active, "it's closed", fresh]`, 1), `default: "'active'"`, `default: "'fresh'"`, 1),
		"enum to text":    strings.Replace(enumTables, "  - name: state\n    type: status\n    default: \"'active'\"", "  - name: state\n    type: text\n    default: \"'active'\"", 1),
		"array to scalar": strings.Replace(enumTables, "type: integer[]", "type: integer", 1),
	}
	for label, y := range cases {
		p, err := plan.Build(src, enumDocument(t, name, y), nil)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if p.Status != "blocked" {
			t.Errorf("%s: not blocked\n%s", label, sqlOf(p))
		}
	}
	// Dropping the enum with everything that uses it is blocked too.
	p, err := plan.Build(src, enumDocument(t, name, "tables: []\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range p.Blocked {
		if b.Object == "enum status" {
			found = true
		}
	}
	if !found {
		t.Fatalf("enum drop not blocked: %+v", p.Blocked)
	}
}

func TestInspectRefusesOtherTypes(t *testing.T) {
	db := database(t)
	for label, ddl := range map[string]string{
		"domain":         "CREATE DOMAIN %s.posint AS integer CHECK (VALUE > 0)",
		"foreign enum":   "CREATE TYPE public.fs_mood_%s AS ENUM ('a'); CREATE TABLE %s.t (id integer PRIMARY KEY, m public.fs_mood_%s)",
		"2-d array":      "CREATE TABLE %s.t (id integer PRIMARY KEY, m integer[][])",
		"array of point": "CREATE TABLE %s.t (id integer PRIMARY KEY, m point[])",
		"composite type": "CREATE TYPE %s.pair AS (a integer, b integer)",
		"array default":  "CREATE TABLE %s.t (id integer PRIMARY KEY, m integer[] DEFAULT '{1}')",
	} {
		name := isolated(t, db)
		q := strings.ReplaceAll(ddl, "%s", name)
		exec(t, db, q)
		if label == "foreign enum" {
			t.Cleanup(func() { db.Exec("DROP TYPE IF EXISTS public.fs_mood_" + name + " CASCADE") })
		}
		if _, err := postgres.Inspect(context.Background(), db, name); err == nil {
			t.Errorf("%s: inspection accepted it", label)
		}
	}
}
