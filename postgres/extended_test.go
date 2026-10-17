package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ForgeSchema/forgeschema/internal/cli"
	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
)

func shopDocument(t *testing.T, name string) schema.Document {
	t.Helper()
	f, err := os.Open("../examples/shop.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := schema.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	d.Schema = name
	return d
}

// shopDatabase creates the shop schema from hand-written DDL with sample rows.
func shopDatabase(t *testing.T, db *sql.DB) string {
	t.Helper()
	name := isolated(t, db)
	ddl, err := os.ReadFile("../testdata/postgres/002-shop.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, db, strings.ReplaceAll(string(ddl), "public.", name+"."))
	return name
}

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("DATABASE_URL", os.Getenv("FORGESCHEMA_TEST_DATABASE_URL"))
	var out, errs bytes.Buffer
	code := cli.Run(args, &out, &errs)
	return code, out.String(), errs.String()
}

func planFile(t *testing.T, p plan.Plan) string {
	t.Helper()
	b, err := p.JSON()
	if err != nil {
		t.Fatal(err)
	}
	return writeTestFile(t, "plan.json", b)
}

func TestInspectShopMatchesContract(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	got, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := shopDocument(t, name).CanonicalJSON()
	actual, _ := got.CanonicalJSON()
	if !bytes.Equal(want, actual) {
		t.Fatalf("inspection differs from examples/shop.yaml\nwant %s\ngot  %s", want, actual)
	}
	result, err := postgres.Verify(context.Background(), db, shopDocument(t, name))
	if err != nil || result.Status != "match" {
		t.Fatalf("verify: %+v %v", result, err)
	}
	// Drift in the new structures is reported, not rejected.
	exec(t, db, "ALTER TABLE "+name+".customers ALTER COLUMN status SET DEFAULT 'paused'; ALTER TABLE "+name+".orders DROP CONSTRAINT orders_customer_fkey")
	result, err = postgres.Verify(context.Background(), db, shopDocument(t, name))
	if err != nil || result.Status != "drift" || len(result.Differences) != 2 ||
		result.Differences[0].Kind != "column_default" || result.Differences[1].Kind != "foreign_key_missing" {
		t.Fatalf("drift: %+v %v", result, err)
	}
}

func TestInspectRejectsCrossSchemaForeignKey(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	other := isolated(t, db)
	exec(t, db, "CREATE TABLE "+other+".p(id integer PRIMARY KEY); CREATE TABLE "+name+".c(p integer REFERENCES "+other+".p(id))")
	_, err := postgres.Inspect(context.Background(), db, name)
	var unsupported *postgres.UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("cross-schema foreign key accepted: %v", err)
	}
}

func TestApplyShopFromEmptySchema(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.Build(src, shopDocument(t, name), nil)
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	result, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{})
	if err != nil || result.Status != "applied" || result.Operations != 7 {
		t.Fatalf("%+v %v", result, err)
	}
	if v, err := postgres.Verify(context.Background(), db, shopDocument(t, name)); err != nil || v.Status != "match" {
		t.Fatalf("verify after apply: %+v %v", v, err)
	}
	// The created defaults, identity, keys and actions behave as declared.
	exec(t, db, "INSERT INTO "+name+".customers(email) VALUES ('a@example.com')")
	exec(t, db, "INSERT INTO "+name+".orders(customer_id) VALUES (1)")
	var id int64
	var status string
	var total string
	var paid bool
	if err = db.QueryRow("SELECT c.id,c.status,o.total::text,o.paid FROM "+name+".customers c JOIN "+name+".orders o ON o.customer_id=c.id").Scan(&id, &status, &total, &paid); err != nil {
		t.Fatal(err)
	}
	if id != 1 || status != "active" || total != "0.00" || paid {
		t.Fatalf("defaults: %d %s %s %v", id, status, total, paid)
	}
	if _, err = db.Exec("INSERT INTO " + name + ".customers(email) VALUES ('a@example.com')"); err == nil {
		t.Fatal("unique constraint missing")
	}
	if _, err = db.Exec("DELETE FROM " + name + ".customers"); err == nil {
		t.Fatal("ON DELETE RESTRICT missing")
	}
}

func shopChange(t *testing.T, db *sql.DB, name string, change func(*schema.Document)) plan.Plan {
	t.Helper()
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	dst, _ := schema.Normalize(src)
	change(&dst)
	p, err := plan.Build(src, dst, nil)
	if err != nil || p.Status != "ready" {
		t.Fatalf("plan: %+v %v", p.Blocked, err)
	}
	return p
}
func column(d *schema.Document, table, name string) *schema.Column {
	for ti := range d.Tables {
		if d.Tables[ti].Name != table {
			continue
		}
		for ci := range d.Tables[ti].Columns {
			if d.Tables[ti].Columns[ci].Name == name {
				return &d.Tables[ti].Columns[ci]
			}
		}
	}
	return nil
}
func table(d *schema.Document, name string) *schema.Table {
	for ti := range d.Tables {
		if d.Tables[ti].Name == name {
			return &d.Tables[ti]
		}
	}
	return nil
}

func TestApplyColumnChangesOnPopulatedTables(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	p := shopChange(t, db, name, func(d *schema.Document) {
		c := table(d, "customers")
		c.Columns = append(c.Columns, schema.Column{Name: "tier", Type: "text", Default: "'basic'"}, schema.Column{Name: "token", Type: "uuid", Default: "gen_random_uuid()"})
		column(d, "customers", "status").Type = "varchar(40)"
		column(d, "customers", "email").Type = "text"
		column(d, "customers", "created_at").Default = "current_timestamp"
		column(d, "orders", "metadata").Nullable = true
		column(d, "orders", "metadata").Default = ""
		column(d, "order_lines", "quantity").Default = "2"
	})
	if _, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if v, err := postgres.Verify(context.Background(), db, p.Target); err != nil || v.Status != "match" {
		t.Fatalf("%+v %v", v, err)
	}
	var tiers, tokens int
	if err := db.QueryRow("SELECT count(*) FILTER (WHERE tier='basic'),count(DISTINCT token) FROM "+name+".customers").Scan(&tiers, &tokens); err != nil || tiers != 2 || tokens != 2 {
		t.Fatalf("existing rows not filled: %d %d %v", tiers, tokens, err)
	}
	// SET NOT NULL fails on the NULL display_name and rolls everything back.
	failing := shopChange(t, db, name, func(d *schema.Document) {
		column(d, "customers", "display_name").Nullable = false
		c := table(d, "customers")
		c.Columns = append(c.Columns, schema.Column{Name: "never", Type: "text", Nullable: true})
	})
	code, _, stderr := runCLI(t, "apply", planFile(t, failing))
	if code != 1 || !strings.Contains(stderr, "contains null values") || !strings.Contains(stderr, "SQLSTATE 23502") {
		t.Fatalf("expected a clear rollback message, got %d %s", code, stderr)
	}
	if v, err := postgres.Verify(context.Background(), db, p.Target); err != nil || v.Status != "match" {
		t.Fatalf("failed apply left changes behind: %+v %v", v, err)
	}
}

func TestApplyConstraintsOnPopulatedTables(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	p := shopChange(t, db, name, func(d *schema.Document) {
		o := table(d, "orders")
		o.Columns = append(o.Columns, schema.Column{Name: "reference", Type: "varchar(30)", Nullable: true})
		o.Unique = append(o.Unique, schema.Unique{Name: "orders_reference_key", Columns: []string{"reference"}})
		c := table(d, "customers")
		c.Columns = append(c.Columns, schema.Column{Name: "referrer_id", Type: "bigint", Nullable: true})
		c.ForeignKeys = append(c.ForeignKeys, schema.ForeignKey{Name: "customers_referrer_fkey", Columns: []string{"referrer_id"}, References: schema.Reference{Table: "customers", Columns: []string{"id"}}, OnDelete: "set_null"})
	})
	if _, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if v, err := postgres.Verify(context.Background(), db, p.Target); err != nil || v.Status != "match" {
		t.Fatalf("%+v %v", v, err)
	}
	// A unique constraint over duplicate data fails with the server's reason.
	exec(t, db, "UPDATE "+name+".customers SET display_name='Same'")
	dup := shopChange(t, db, name, func(d *schema.Document) {
		c := table(d, "customers")
		c.Unique = append(c.Unique, schema.Unique{Name: "customers_display_name_key", Columns: []string{"display_name"}})
	})
	code, _, stderr := runCLI(t, "apply", planFile(t, dup))
	if code != 1 || !strings.Contains(stderr, "SQLSTATE 23505") {
		t.Fatalf("duplicate unique: %d %s", code, stderr)
	}
}

func TestApplyLocksOnlyChangedTables(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	p := shopChange(t, db, name, func(d *schema.Document) {
		c := table(d, "customers")
		c.Columns = append(c.Columns, schema.Column{Name: "nickname", Type: "text", Nullable: true})
	})
	// Another session keeps order_lines busy; the plan does not touch it.
	external, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer external.Rollback()
	if _, err = external.Exec("LOCK TABLE " + name + ".order_lines IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	if _, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{LockTimeout: 500 * time.Millisecond}); err != nil {
		t.Fatalf("apply waited on an unchanged table: %v", err)
	}
}

func TestApplyCLIExitCodesAndTimeouts(t *testing.T) {
	db := database(t)
	name := shopDatabase(t, db)
	p := shopChange(t, db, name, func(d *schema.Document) {
		c := table(d, "customers")
		c.Columns = append(c.Columns, schema.Column{Name: "nickname", Type: "text", Nullable: true})
	})
	path := planFile(t, p)
	if code, _, stderr := runCLI(t, "apply", "--lock-timeout", "0s", path); code != 2 || !strings.Contains(stderr, "at least 1ms") {
		t.Fatalf("zero timeout: %d %s", code, stderr)
	}
	if code, _, _ := runCLI(t, "apply", "--bogus", path); code != 2 {
		t.Fatal("unknown flag accepted")
	}
	// A held lock surfaces PostgreSQL's lock timeout, with its SQLSTATE.
	external, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer external.Rollback()
	if _, err = external.Exec("LOCK TABLE " + name + ".customers IN ACCESS SHARE MODE"); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI(t, "apply", "--lock-timeout", "100ms", "--statement-timeout", "2m", "--timeout", "5m", path)
	if code != 1 || !strings.Contains(stderr, "relation locks") || !strings.Contains(stderr, "SQLSTATE 55P03") {
		t.Fatalf("lock timeout: %d %s", code, stderr)
	}
	if err = external.Rollback(); err != nil {
		t.Fatal(err)
	}
	// Drift between planning and apply exits 5.
	exec(t, db, "ALTER TABLE "+name+".orders ADD COLUMN surprise text")
	code, stdout, stderr := runCLI(t, "apply", path)
	if code != 5 || stdout != "" || !strings.Contains(stderr, "no longer matches") {
		t.Fatalf("drift: %d %s", code, stderr)
	}
}

// A role, database or connection may turn standard_conforming_strings off.
// Apply and inspect set it on for their own transactions, so defaults keep
// their meaning and quotes round-trip.
func TestStandardStringsOffConnection(t *testing.T) {
	database(t)
	dsn := os.Getenv("FORGESCHEMA_TEST_DATABASE_URL")
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, err := sql.Open("postgres", dsn+sep+"options=-c%20standard_conforming_strings%3Doff")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() }) // after isolated's schema cleanup
	var setting string
	if err = db.QueryRow("SHOW standard_conforming_strings").Scan(&setting); err != nil || setting != "off" {
		t.Fatalf("setting not applied: %q %v", setting, err)
	}
	name := isolated(t, db)
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	dst := shopDocument(t, name)
	dst.Tables[0].Columns = append(dst.Tables[0].Columns, schema.Column{Name: "motto", Type: "text", Default: "'it''s fine'"})
	p, err := plan.Build(src, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if v, err := postgres.Verify(context.Background(), db, dst); err != nil || v.Status != "match" {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestTypeChangeLocksForeignKeyPartnerFirst(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	exec(t, db, "CREATE TABLE "+name+".p(code varchar(5) PRIMARY KEY); CREATE TABLE "+name+".c(code varchar(5) NOT NULL CONSTRAINT c_code_fkey REFERENCES "+name+".p(code))")
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	dst, _ := schema.Normalize(src)
	column(&dst, "p", "code").Type = "varchar(10)"
	p, err := plan.Build(src, dst, nil)
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	reader, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback()
	if _, err = reader.Exec("SELECT * FROM " + name + ".c"); err != nil {
		t.Fatal(err)
	}
	_, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{LockTimeout: 200 * time.Millisecond})
	var stage *postgres.ApplyError
	if !errors.As(err, &stage) || stage.Stage != "relation locks" {
		t.Fatalf("partner table was not locked up front: %v", err)
	}
	if err = reader.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
}
