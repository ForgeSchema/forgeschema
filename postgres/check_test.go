// Copyright 2026 ForgeSchema
// SPDX-License-Identifier: Apache-2.0

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
)

func TestInspectCheckConstraints(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	exec(t, db, `CREATE TABLE `+name+`.t (
 id integer NOT NULL, small smallint, big bigint, price numeric(10,2), label text, code varchar(20), tag char(3), flag boolean, "order" integer,
 CONSTRAINT t_id_check CHECK (id > 0 AND id < 1000000),
 CONSTRAINT t_small_check CHECK (small IN (1, 2, 3)),
 CONSTRAINT t_big_check CHECK (big IN (1, 2) AND big >= -9223372036854775808),
 CONSTRAINT t_price_check CHECK (price >= 0 AND price IN (1, 2.5) AND price > -1.25),
 CONSTRAINT t_label_check CHECK (char_length(label) <= 100 AND label <> ''),
 CONSTRAINT t_code_check CHECK (code IN ('a', 'it''s') AND char_length(code) > 0 AND code <> label),
 CONSTRAINT t_tag_check CHECK (tag = 'abc'),
 CONSTRAINT t_flag_check CHECK (flag = true),
 CONSTRAINT t_order_check CHECK ("order" < big))`)
	got, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"t_id_check":    "id > 0 AND id < 1000000",
		"t_small_check": "small IN (1, 2, 3)",
		"t_big_check":   "big IN (1, 2) AND big >= -9223372036854775808",
		"t_price_check": "price >= 0 AND price IN (1, 2.5) AND price > -1.25",
		"t_label_check": "char_length(label) <= 100 AND label <> ''",
		"t_code_check":  "code IN ('a', 'it''s') AND char_length(code) > 0 AND code <> label",
		"t_tag_check":   "tag = 'abc'",
		"t_flag_check":  "flag = true",
		"t_order_check": "order < big",
	}
	if len(got.Tables[0].Checks) != len(want) {
		t.Fatalf("checks %+v", got.Tables[0].Checks)
	}
	for _, c := range got.Tables[0].Checks {
		if want[c.Name] != c.Expression {
			t.Errorf("%s: got %q, want %q", c.Name, c.Expression, want[c.Name])
		}
	}
	// The same document applied to an empty schema inspects identically.
	other := isolated(t, db)
	empty, err := postgres.Inspect(context.Background(), db, other)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := schema.Normalize(got)
	target.Schema = other
	p, err := plan.Build(empty, target, nil)
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	if _, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if v, err := postgres.Verify(context.Background(), db, target); err != nil || v.Status != "match" {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestApplyChecksOnPopulatedTables(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	p := shopChange(t, db, name, func(d *schema.Document) {
		c := table(d, "customers")
		c.Checks = append(c.Checks, schema.Check{Name: "customers_email_check", Expression: "char_length(email) >= 3"})
		o := table(d, "orders")
		o.Columns = append(o.Columns, schema.Column{Name: "discount", Type: "numeric(5,2)", Default: "0"})
		o.Checks = append(o.Checks, schema.Check{Name: "orders_discount_check", Expression: "discount >= 0 and discount <= total"})
	})
	kinds := map[string]int{}
	for _, op := range p.Operations {
		kinds[op.Kind]++
	}
	if kinds["add_check"] != 2 {
		t.Fatalf("operations %v", kinds)
	}
	if _, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if v, err := postgres.Verify(context.Background(), db, p.Target); err != nil || v.Status != "match" {
		t.Fatalf("%+v %v", v, err)
	}
	// A check that existing rows break fails with the server's reason.
	failing := shopChange(t, db, name, func(d *schema.Document) {
		o := table(d, "orders")
		o.Checks = append(o.Checks, schema.Check{Name: "orders_total_big_check", Expression: "total > 100"})
	})
	code, _, stderr := runCLI(t, "apply", planFile(t, failing))
	if code != 1 || !strings.Contains(stderr, "SQLSTATE 23514") {
		t.Fatalf("violated check: %d %s", code, stderr)
	}
	if v, err := postgres.Verify(context.Background(), db, p.Target); err != nil || v.Status != "match" {
		t.Fatalf("failed apply left changes behind: %+v %v", v, err)
	}
	// Changing or dropping a check is blocked.
	src, _ := postgres.Inspect(context.Background(), db, name)
	dst, _ := schema.Normalize(src)
	table(&dst, "orders").Checks[0].Expression = "total >= 1"
	if blocked, _ := plan.Build(src, dst, nil); blocked.Status != "blocked" {
		t.Fatal("changed check not blocked")
	}
}

func TestRenameAndWidenColumnsWithChecks(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	dst, _ := schema.Normalize(src)
	column(&dst, "customers", "status").Type = "text"
	column(&dst, "order_lines", "quantity").Name = "amount"
	l := table(&dst, "order_lines")
	for i := range l.Checks {
		if l.Checks[i].Name == "order_lines_quantity_check" {
			l.Checks[i].Expression = "amount > 0"
		}
	}
	p, err := plan.Build(src, dst, []plan.Rename{{Table: "order_lines", From: "quantity", To: "amount"}})
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	if _, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if v, err := postgres.Verify(context.Background(), db, p.Target); err != nil || v.Status != "match" {
		t.Fatalf("%+v %v", v, err)
	}
}

// PostgreSQL opens a table to print its check expressions, so inspecting a
// table with checks waits behind a session holding ACCESS EXCLUSIVE on it.
func TestInspectWaitsOnLockedTableWithChecks(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	external, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer external.Rollback()
	if _, err = external.Exec("LOCK TABLE " + name + ".orders IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err = postgres.Inspect(ctx, db, name); err == nil {
		t.Fatal("inspection read a locked table's checks")
	}
}
