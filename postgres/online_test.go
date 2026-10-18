// Copyright 2026 ForgeSchema
// SPDX-License-Identifier: Apache-2.0

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
)

func onlinePlan(t *testing.T, db *sql.DB, name string, change func(*schema.Document)) plan.Plan {
	t.Helper()
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	dst, _ := schema.Normalize(src)
	change(&dst)
	p, err := plan.BuildWith(src, dst, nil, plan.Options{Online: true})
	if err != nil || p.Status != "ready" {
		t.Fatalf("plan: %+v %v", p.Blocked, err)
	}
	return p
}

func indexValid(t *testing.T, db *sql.DB, schemaName, index string) (exists, valid bool) {
	t.Helper()
	err := db.QueryRow(`SELECT i.indisvalid FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 JOIN pg_catalog.pg_index i ON i.indexrelid=c.oid WHERE n.nspname=$1 AND c.relname=$2`, schemaName, index).Scan(&valid)
	if err == sql.ErrNoRows {
		return false, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true, valid
}

func TestOnlineApplyOnPopulatedTables(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	change := func(d *schema.Document) {
		o := table(d, "orders")
		o.Columns = append(o.Columns, schema.Column{Name: "reference", Type: "varchar(30)", Nullable: true})
		o.Unique = append(o.Unique, schema.Unique{Name: "orders_reference_key", Columns: []string{"reference"}})
		c := table(d, "customers")
		c.Indexes = append(c.Indexes, schema.Index{Name: "customers_display_name_idx", Columns: []string{"display_name"}})
		c.Columns = append(c.Columns, schema.Column{Name: "referrer_id", Type: "bigint", Nullable: true})
		c.ForeignKeys = append(c.ForeignKeys, schema.ForeignKey{Name: "customers_referrer_fkey", Columns: []string{"referrer_id"}, References: schema.Reference{Table: "customers", Columns: []string{"id"}}, OnDelete: "set_null"})
	}
	p := onlinePlan(t, db, name, change)
	if offline := shopChange(t, db, name, change); offline.Online {
		t.Fatal("plain plan marked online")
	}
	kinds := map[string]int{}
	for _, op := range p.Operations {
		kinds[op.Kind]++
		if op.Kind == plan.ConcurrentIndex && !strings.Contains(op.SQL, " CONCURRENTLY ") {
			t.Fatalf("not concurrent: %s", op.SQL)
		}
	}
	if kinds[plan.ConcurrentIndex] != 2 || kinds["attach_unique"] != 1 {
		t.Fatalf("operations: %v", kinds)
	}
	result, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{})
	if err != nil || result.Status != "applied" {
		t.Fatalf("%+v %v", result, err)
	}
	if v, err := postgres.Verify(context.Background(), db, p.Target); err != nil || v.Status != "match" {
		t.Fatalf("%+v %v", v, err)
	}
	id, _ := p.ID()
	if historyCount(t, db, id) != 1 {
		t.Fatal("history not recorded once")
	}
	var drift *postgres.DriftError
	if _, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); !errors.As(err, &drift) {
		t.Fatalf("replay: %v", err)
	}

	// A plan that only builds an index looks resumable once it has run; its
	// history row keeps it from being applied a second time.
	only := onlinePlan(t, db, name, func(d *schema.Document) {
		o := table(d, "orders")
		o.Indexes = append(o.Indexes, schema.Index{Name: "orders_paid_idx", Columns: []string{"paid"}})
	})
	if _, err = postgres.Apply(context.Background(), db, only, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = postgres.Apply(context.Background(), db, only, postgres.ApplyOptions{}); !errors.As(err, &drift) {
		t.Fatalf("index-only replay: %v", err)
	}
	onlyID, _ := only.ID()
	if historyCount(t, db, onlyID) != 1 {
		t.Fatal("index-only replay changed history")
	}
}

func TestOnlineApplyResumesAfterFailedBuild(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	exec(t, db, "UPDATE "+name+".customers SET display_name='Same'")
	p := onlinePlan(t, db, name, func(d *schema.Document) {
		c := table(d, "customers")
		c.Columns = append(c.Columns, schema.Column{Name: "note", Type: "text", Nullable: true})
		c.Unique = append(c.Unique, schema.Unique{Name: "customers_display_name_key", Columns: []string{"display_name"}})
		o := table(d, "orders")
		o.Indexes = append(o.Indexes, schema.Index{Name: "orders_paid_idx", Columns: []string{"paid"}})
	})
	path := planFile(t, p)
	code, stdout, stderr := runCLI(t, "apply", path)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "SQLSTATE 23505") || !strings.Contains(stderr, "apply the same plan again") {
		t.Fatalf("failed build: %d %s", code, stderr)
	}
	if exists, valid := indexValid(t, db, name, "customers_display_name_key"); !exists || valid {
		t.Fatalf("expected an invalid index left behind: %v %v", exists, valid)
	}
	id, _ := p.ID()
	if historyCount(t, db, id) != 0 {
		t.Fatal("history written for an unfinished plan")
	}
	var note bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=$1 AND table_name='customers' AND column_name='note')", name).Scan(&note); err != nil || !note {
		t.Fatalf("first phase not committed: %v", err)
	}

	// Any other change to the schema in between is still drift.
	exec(t, db, "CREATE TABLE "+name+".stray(id integer)")
	var drift *postgres.DriftError
	if _, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); !errors.As(err, &drift) {
		t.Fatalf("drift during resume: %v", err)
	}
	exec(t, db, "DROP TABLE "+name+".stray")

	exec(t, db, "UPDATE "+name+".customers SET display_name='n'||id")
	code, stdout, stderr = runCLI(t, "apply", path)
	if code != 0 || !strings.Contains(stdout, `"applied"`) {
		t.Fatalf("resume: %d %s %s", code, stdout, stderr)
	}
	if v, err := postgres.Verify(context.Background(), db, p.Target); err != nil || v.Status != "match" {
		t.Fatalf("%+v %v", v, err)
	}
	if historyCount(t, db, id) != 1 {
		t.Fatal("history not recorded once")
	}
}

// A concurrent build must not run inside the first phase's transaction, and
// readers and writers of the table keep working while it runs.
func TestOnlineBuildDoesNotBlockWriters(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	p := onlinePlan(t, db, name, func(d *schema.Document) {
		o := table(d, "orders")
		o.Indexes = append(o.Indexes, schema.Index{Name: "orders_total_idx", Columns: []string{"total"}})
	})
	exclusive, _ := p.LockedTables()
	if len(exclusive) != 0 {
		t.Fatalf("online index build locks %v", exclusive)
	}
	writer, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Exec("INSERT INTO " + name + ".orders(customer_id,total) VALUES (1, 5)"); err != nil {
		t.Fatal(err)
	}
	// The build waits for the open writer, so commit it shortly after.
	done := make(chan error, 1)
	go func() {
		// The build's wait for the writer outlasts lock_timeout, which does
		// not apply to it.
		_, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{LockTimeout: 100 * time.Millisecond})
		done <- err
	}()
	waitForBuild(t, db)
	time.Sleep(500 * time.Millisecond)
	if err = writer.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if v, err := postgres.Verify(context.Background(), db, p.Target); err != nil || v.Status != "match" {
		t.Fatalf("%+v %v", v, err)
	}
}

func waitForBuild(t *testing.T, db *sql.DB) {
	t.Helper()
	for i := 0; i < 200; i++ {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM pg_catalog.pg_stat_progress_create_index").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("index build never started")
}

// Another apply waiting for the coordination lock must not hold a snapshot,
// or the concurrent build waits for it while it waits for the build.
func TestOnlineBuildAndWaitingApplyDoNotDeadlock(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	_, other := applyFixture(t, db)
	p := onlinePlan(t, db, name, func(d *schema.Document) {
		o := table(d, "orders")
		o.Indexes = append(o.Indexes, schema.Index{Name: "orders_total_idx", Columns: []string{"total"}})
	})
	writer, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	if _, err = writer.Exec("INSERT INTO " + name + ".orders(customer_id,total) VALUES (1, 5)"); err != nil {
		t.Fatal(err)
	}
	online, plain := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{})
		online <- err
	}()
	waitForBuild(t, db)
	go func() {
		_, err := postgres.Apply(context.Background(), db, other, postgres.ApplyOptions{LockTimeout: 20 * time.Second, Timeout: 30 * time.Second})
		plain <- err
	}()
	time.Sleep(300 * time.Millisecond)
	if err = writer.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-online; err != nil {
		t.Fatalf("online apply: %v", err)
	}
	if err = <-plain; err != nil {
		t.Fatalf("waiting apply: %v", err)
	}
}
