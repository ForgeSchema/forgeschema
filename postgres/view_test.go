package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
)

const viewTables = `enums:
- name: status
  values: [active, closed]
tables:
- name: orders
  columns:
  - name: id
    type: bigint
  - name: state
    type: status
    default: "'active'"
  - name: tags
    type: text[]
    default: "'{}'"
  - name: total
    type: integer
    nullable: true
  primary_key: [id]
views:
- name: open_orders
  query: |
    SELECT id,
        state,
        tags
       FROM orders
      WHERE state = 'active'::status;
  depends_on: [orders]
- name: open_count
  query: SELECT count(*) AS n, max(id) AS last_id FROM open_orders
  depends_on: [open_orders]
`

func kinds(p plan.Plan) string {
	var out []string
	for _, op := range p.Operations {
		out = append(out, op.Kind+" "+op.Table+op.Subject)
	}
	return strings.Join(out, ", ")
}

func TestViewsRoundTrip(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	want := enumDocument(t, name, viewTables)
	p := applyPlan(t, name, want)
	if got := kinds(p); got != "create_enum status, create_table orders, create_view open_orders, create_view open_count" {
		t.Fatalf("operations: %s", got)
	}
	got, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := got.CanonicalJSON()
	b, _ := want.CanonicalJSON()
	if string(a) != string(b) {
		t.Fatalf("inspect differs\n got  %s\n want %s", a, b)
	}
	exec(t, db, "INSERT INTO "+name+".orders(id) VALUES (1), (2)")
	exec(t, db, "INSERT INTO "+name+".orders(id, state) VALUES (3, 'closed')")
	var n, last int
	if err := db.QueryRow("SELECT n, last_id FROM "+name+".open_count").Scan(&n, &last); err != nil || n != 2 || last != 2 {
		t.Fatalf("open_count %d %d %v", n, last, err)
	}

	// Changing the first view drops and creates both, in order.
	next := strings.Replace(viewTables, "        tags\n", "        tags,\n        total\n", 1)
	p = applyPlan(t, name, enumDocument(t, name, next))
	if got := kinds(p); got != "drop_view open_count, drop_view open_orders, create_view open_orders, create_view open_count" {
		t.Fatalf("operations: %s", got)
	}
	if !strings.Contains(p.Operations[0].Explanation, "reads open_orders") {
		t.Fatalf("explanation: %s", p.Operations[0].Explanation)
	}

	// A rename on the table the views read drops and creates them around it.
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	renamed := strings.NewReplacer("  - name: total\n", "  - name: amount\n", "        total\n", "        amount\n").Replace(next)
	p, err = plan.Build(src, enumDocument(t, name, renamed), []plan.Rename{{Table: "orders", From: "total", To: "amount"}})
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	if got := kinds(p); got != "drop_view open_count, drop_view open_orders, rename_column orderstotal, create_view open_orders, create_view open_count" {
		t.Fatalf("operations: %s", got)
	}
	if _, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}

	// A view the target leaves out is dropped; the one that reads nothing
	// changed stays.
	removed := strings.Replace(renamed, renamed[strings.Index(renamed, "- name: open_count"):], "", 1)
	p = applyPlan(t, name, enumDocument(t, name, removed))
	if got := kinds(p); got != "drop_view open_count" {
		t.Fatalf("operations: %s", got)
	}
	if !strings.Contains(p.Operations[0].Explanation, "no longer has") {
		t.Fatalf("explanation: %s", p.Operations[0].Explanation)
	}
}

func TestViewQueryInAnotherForm(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	loose := strings.Replace(viewTables, "WHERE state = 'active'::status;", "where state = 'active'", 1)
	want := enumDocument(t, name, loose)
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.Build(src, want, nil)
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	_, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{})
	var drift *postgres.DriftError
	if !errors.As(err, &drift) {
		t.Fatalf("want drift, got %v", err)
	}
	if !strings.Contains(err.Error(), `view open_orders: PostgreSQL stores the query as "SELECT id, state, tags FROM orders WHERE state = 'active'::status"`) {
		t.Fatalf("message: %v", err)
	}
	// Nothing was applied.
	after, err := postgres.Inspect(context.Background(), db, name)
	if err != nil || len(after.Tables) != 0 {
		t.Fatalf("schema changed: %+v %v", after, err)
	}
}

func TestViewDependsOnChecked(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	wrong := strings.Replace(viewTables, "  depends_on: [open_orders]\n", "  depends_on: [open_orders, orders]\n", 1)
	src, _ := postgres.Inspect(context.Background(), db, name)
	p, err := plan.Build(src, enumDocument(t, name, wrong), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "view open_count: view_depends_on") {
		t.Fatalf("want depends_on drift, got %v", err)
	}
}

func TestOnlinePlanWithViews(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	applyPlan(t, name, enumDocument(t, name, viewTables))
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	next := strings.Replace(viewTables, "  primary_key: [id]\n", "  primary_key: [id]\n  indexes:\n  - name: orders_total\n    columns: [total]\n", 1)
	next = strings.Replace(next, "        tags\n", "        tags,\n        total\n", 1)
	want := enumDocument(t, name, next)
	p, err := plan.BuildWith(src, want, nil, plan.Options{Online: true})
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	first, concurrent, final := p.Phases()
	if len(first) != 2 || len(concurrent) != 1 || len(final) != 2 || final[0].Kind != "create_view" {
		t.Fatalf("phases: %s", kinds(p))
	}
	if _, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if v, err := postgres.Verify(context.Background(), db, want); err != nil || v.Status != "match" {
		t.Fatalf("verify: %+v %v", v, err)
	}
}

func TestVerifyReportsViewDrift(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	want := enumDocument(t, name, viewTables)
	applyPlan(t, name, want)
	exec(t, db, "CREATE OR REPLACE VIEW "+name+".open_orders AS SELECT id, state, tags FROM "+name+".orders WHERE state = 'closed'")
	v, err := postgres.Verify(context.Background(), db, want)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status == "match" || len(v.Differences) != 1 || v.Differences[0].Kind != "view_query" || v.Differences[0].View != "open_orders" {
		t.Fatalf("verify: %+v", v)
	}
}

func TestInspectRefusesOtherViews(t *testing.T) {
	db := database(t)
	for label, ddl := range map[string]string{
		"materialized":     "CREATE MATERIALIZED VIEW %s.v AS SELECT 1 AS id",
		"check option":     "CREATE TABLE %s.t (id integer PRIMARY KEY); CREATE VIEW %s.v AS SELECT id FROM %s.t WITH CHECK OPTION",
		"security barrier": "CREATE VIEW %s.v WITH (security_barrier) AS SELECT 1 AS id",
		"other schema":     "CREATE TABLE public.fs_other_%s (id integer); CREATE VIEW %s.v AS SELECT id FROM public.fs_other_%s",
		"rule":             "CREATE TABLE %s.t (id integer PRIMARY KEY); CREATE VIEW %s.v AS SELECT id FROM %s.t; CREATE RULE r AS ON INSERT TO %s.v DO INSTEAD NOTHING",
		"column default":   "CREATE TABLE %s.t (id integer PRIMARY KEY); CREATE VIEW %s.v AS SELECT id FROM %s.t; ALTER VIEW %s.v ALTER COLUMN id SET DEFAULT 1",
		"backslash":        `CREATE VIEW %s.v AS SELECT 'a\b'::text AS s`,
	} {
		name := isolated(t, db)
		exec(t, db, strings.ReplaceAll(ddl, "%s", name))
		if label == "other schema" {
			t.Cleanup(func() { db.Exec("DROP TABLE IF EXISTS public.fs_other_" + name + " CASCADE") })
		}
		if _, err := postgres.Inspect(context.Background(), db, name); err == nil {
			t.Errorf("%s: inspection accepted it", label)
		}
	}
}

func TestViewDocumentRules(t *testing.T) {
	base := "version: 1\nschema: shop\ntables:\n- name: t\n  columns:\n  - name: id\n    type: integer\n"
	for label, views := range map[string]string{
		"two statements": "- name: v\n  query: SELECT 1; DROP TABLE t\n",
		"comment":        "- name: v\n  query: SELECT 1 -- hi\n",
		"dollar":         "- name: v\n  query: SELECT $$a$$\n",
		"unknown dep":    "- name: v\n  query: SELECT 1\n  depends_on: [nope]\n",
		"self dep":       "- name: v\n  query: SELECT 1\n  depends_on: [v]\n",
		"cycle":          "- name: v\n  query: SELECT 1\n  depends_on: [w]\n- name: w\n  query: SELECT 1\n  depends_on: [v]\n",
		"table clash":    "- name: t\n  query: SELECT 1\n",
		"type clash":     "- name: _t\n  query: SELECT 1\n",
		"unterminated":   "- name: v\n  query: SELECT 'a\n",
	} {
		if _, err := schema.Load(strings.NewReader(base + "views:\n" + views)); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
	d, err := schema.Load(strings.NewReader(base + "views:\n- name: v\n  query: \"SELECT  'a  b;' ,\\n  \\\"x  y\\\" FROM t ;\"\n  depends_on: [t]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Views[0].Query != `SELECT 'a  b;' , "x  y" FROM t` {
		t.Fatalf("query %q", d.Views[0].Query)
	}
	empty, _ := schema.Load(strings.NewReader(base))
	if j, _ := empty.CanonicalJSON(); strings.Contains(string(j), "views") {
		t.Fatal("empty views appear in canonical JSON")
	}
}
