// Copyright 2026 ForgeSchema
// SPDX-License-Identifier: Apache-2.0

package postgres_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
)

// A staged type change: add the new column, backfill it, then drop the old
// one and rename the new one into its place, all through the CLI.
func TestStagedConversionThroughCLI(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	exec(t, db, "INSERT INTO "+name+".order_lines(order_id, line_no, sku, quantity) SELECT id, 2, 'SKU-00000002', 7 FROM "+name+".orders")
	writeDoc := func(file string, d schema.Document) string {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		return writeTestFile(t, file, b)
	}
	inspect := func() schema.Document {
		d, err := postgres.Inspect(context.Background(), db, name)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	planAndApply := func(src, dst schema.Document, changes string) {
		t.Helper()
		args := []string{"plan", writeDoc("current.json", src), writeDoc("desired.json", dst)}
		if changes != "" {
			args = append(args, writeTestFile(t, "changes.yaml", []byte(changes)))
		}
		code, out, stderr := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("plan: %d %s %s", code, out, stderr)
		}
		path := writeTestFile(t, "plan.json", []byte(out))
		if code, out, stderr = runCLI(t, "apply", path); code != 0 {
			t.Fatalf("apply: %d %s %s", code, out, stderr)
		}
		if code, out, stderr = runCLI(t, "verify", "--plan", path); code != 0 {
			t.Fatalf("verify: %d %s %s", code, out, stderr)
		}
	}

	// Stage 1, expand: a nullable bigint beside the integer column.
	src := inspect()
	expand, _ := schema.Normalize(src)
	l := table(&expand, "order_lines")
	l.Columns = append(l.Columns, schema.Column{Name: "quantity_new", Type: "bigint", Nullable: true})
	planAndApply(src, expand, "")

	// Backfill, outside ForgeSchema.
	exec(t, db, "UPDATE "+name+".order_lines SET quantity_new = quantity")

	// Stage 2, contract: drop the old column and its check, rename the new
	// column into its place, make it required and restore default and check.
	src = inspect()
	contract, _ := schema.Normalize(src)
	l = table(&contract, "order_lines")
	var cols []schema.Column
	for _, c := range l.Columns {
		switch c.Name {
		case "quantity":
		case "quantity_new":
			cols = append(cols, schema.Column{Name: "quantity", Type: "bigint", Default: "1"})
		default:
			cols = append(cols, c)
		}
	}
	l.Columns = cols
	for i := range l.Checks {
		if l.Checks[i].Name == "order_lines_quantity_check" {
			l.Checks[i].Name = "order_lines_quantity_positive"
		}
	}
	planAndApply(src, contract, `version: 1
columns:
- table: order_lines
  from: quantity_new
  to: quantity
drops:
- table: order_lines
  column: quantity
- table: order_lines
  check: order_lines_quantity_check
`)
	var total, rows int
	if err := db.QueryRow("SELECT sum(quantity), count(*) FROM " + name + ".order_lines").Scan(&total, &rows); err != nil || total != 8 || rows != 2 {
		t.Fatalf("data not carried over: %d %d %v", total, rows, err)
	}
	if got := column(ptr(inspect()), "order_lines", "quantity"); got == nil || got.Type != "bigint" || got.Nullable {
		t.Fatalf("column %+v", got)
	}
}

func ptr(d schema.Document) *schema.Document { return &d }

func TestInPlaceConversions(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	exec(t, db, "CREATE TABLE "+name+".t (id integer PRIMARY KEY, code text NOT NULL DEFAULT '7', amount numeric(10,3), label text); INSERT INTO "+name+".t VALUES (1, '12', 1.235, 'short'), (2, '7', 2.5, 'much too long')")
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	dst, _ := schema.Normalize(src)
	column(&dst, "t", "code").Type = "integer"
	column(&dst, "t", "code").Default = "7"
	column(&dst, "t", "amount").Type = "numeric(10,2)"
	column(&dst, "t", "label").Type = "varchar(5)"
	conv := []plan.Conversion{{Table: "t", Column: "code"}, {Table: "t", Column: "amount"}, {Table: "t", Column: "label"}}
	p, err := plan.BuildWith(src, dst, nil, plan.Options{Conversions: conv})
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	// 'much too long' does not fit varchar(5): everything rolls back.
	code, _, stderr := runCLI(t, "apply", planFile(t, p))
	if code != 1 || !strings.Contains(stderr, "SQLSTATE 22001") {
		t.Fatalf("too long: %d %s", code, stderr)
	}
	exec(t, db, "UPDATE "+name+".t SET label='long' WHERE id=2")
	src, _ = postgres.Inspect(context.Background(), db, name)
	if p, err = plan.BuildWith(src, dst, nil, plan.Options{Conversions: conv}); err != nil {
		t.Fatal(err)
	}
	if _, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if v, err := postgres.Verify(context.Background(), db, dst); err != nil || v.Status != "match" {
		t.Fatalf("%+v %v", v, err)
	}
	var codes int
	var amount string
	if err = db.QueryRow("SELECT sum(code), (SELECT amount::text FROM "+name+".t WHERE id=1) FROM "+name+".t").Scan(&codes, &amount); err != nil || codes != 19 || amount != "1.24" {
		t.Fatalf("converted values: %d %s %v", codes, amount, err)
	}
	// A value that does not convert fails the whole migration.
	exec(t, db, "ALTER TABLE "+name+".t ADD COLUMN n text; UPDATE "+name+".t SET n = 'x'")
	src, _ = postgres.Inspect(context.Background(), db, name)
	next, _ := schema.Normalize(src)
	column(&next, "t", "n").Type = "integer"
	p, err = plan.BuildWith(src, next, nil, plan.Options{Conversions: []plan.Conversion{{Table: "t", Column: "n"}}})
	if err != nil {
		t.Fatal(err)
	}
	if code, _, stderr = runCLI(t, "apply", planFile(t, p)); code != 1 || !strings.Contains(stderr, "SQLSTATE 22P02") {
		t.Fatalf("invalid input: %d %s", code, stderr)
	}
}

func TestDropTables(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	dst, _ := schema.Normalize(src)
	dst.Tables = []schema.Table{*table(&dst, "customers")}
	p, err := plan.BuildWith(src, dst, nil, plan.Options{Drops: []plan.Drop{{Table: "orders"}, {Table: "order_lines"}}})
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	if exclusive, _ := p.LockedTables(); strings.Join(exclusive, ",") != "customers,order_lines,orders" {
		t.Fatalf("locks %v", exclusive)
	}
	if _, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if v, err := postgres.Verify(context.Background(), db, dst); err != nil || v.Status != "match" {
		t.Fatalf("%+v %v", v, err)
	}
	var customers int
	if err = db.QueryRow("SELECT count(*) FROM " + name + ".customers").Scan(&customers); err != nil || customers != 2 {
		t.Fatalf("kept table changed: %d %v", customers, err)
	}
}
