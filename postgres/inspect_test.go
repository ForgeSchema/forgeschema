package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ForgeSchema/forgeschema/internal/cli"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
)

func database(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("FORGESCHEMA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set FORGESCHEMA_TEST_DATABASE_URL to a disposable PostgreSQL 16/17 database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var version string
	if err = db.QueryRow("SHOW server_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Log("PostgreSQL", version)
	return db
}
func exec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
}
func isolated(t *testing.T, db *sql.DB) string {
	t.Helper()
	name := fmt.Sprintf("fs_test_%d", time.Now().UnixNano())
	exec(t, db, `CREATE SCHEMA "`+name+`"`)
	t.Cleanup(func() {
		_, err := db.Exec(`DROP SCHEMA "` + name + `" CASCADE`)
		if err != nil {
			t.Error(err)
		}
	})
	return name
}
func TestInspectFixture(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	fixture, err := os.ReadFile("../testdata/postgres/001-customers.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, db, strings.ReplaceAll(string(fixture), "public.", name+"."))
	f, err := os.Open("../examples/customers.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	desired, err := schema.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	desired.Schema = name
	want, _ := desired.CanonicalJSON()
	for i := 0; i < 3; i++ {
		got, err := postgres.Inspect(context.Background(), db, name)
		if err != nil {
			t.Fatal(err)
		}
		actual, _ := got.CanonicalJSON()
		if !bytes.Equal(want, actual) {
			t.Fatalf("snapshot mismatch\nwant %s\ngot  %s", want, actual)
		}
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM " + name + ".customers").Scan(&count); err != nil || count != 2 {
		t.Fatalf("fixture changed: %d %v", count, err)
	}
	t.Setenv("DATABASE_URL", os.Getenv("FORGESCHEMA_TEST_DATABASE_URL"))
	var out, errs bytes.Buffer
	if code := cli.Run([]string{"inspect", name}, &out, &errs); code != 0 || strings.TrimSpace(out.String()) != string(want) {
		t.Fatalf("CLI failed: %d %s %s", code, out.String(), errs.String())
	}
}
func TestEmptyAndMissingSchema(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	d, err := postgres.Inspect(context.Background(), db, name)
	if err != nil || d.Tables == nil || len(d.Tables) != 0 {
		t.Fatalf("empty: %+v %v", d, err)
	}
	_, err = postgres.Inspect(context.Background(), db, name+"_missing")
	var unsupported *postgres.UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("missing schema error: %v", err)
	}
}
func TestSupportedTypesAndOrdering(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	exec(t, db, `CREATE TABLE `+name+`.types (z bigint NOT NULL, a integer NOT NULL, b smallint, c text, d boolean, e uuid, f date, g timestamp, h timestamptz, i jsonb, j bytea, k real, l double precision, PRIMARY KEY(z,a)); CREATE INDEX types_za_idx ON `+name+`.types(z,a)`)
	d, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tables[0].Columns) != 13 || d.Tables[0].Columns[0].Name != "a" || strings.Join(d.Tables[0].PrimaryKey, ",") != "z,a" || strings.Join(d.Tables[0].Indexes[0].Columns, ",") != "z,a" {
		t.Fatalf("wrong ordering: %+v", d)
	}
}
func TestUnsupportedStructures(t *testing.T) {
	cases := map[string]string{
		"generated":            "CREATE TABLE %s.t (id integer, doubled integer GENERATED ALWAYS AS (id*2) STORED)",
		"timestamp-precision":  "CREATE TABLE %s.t (v timestamp(2))",
		"array":                "CREATE TABLE %s.t (v integer[])",
		"domain":               "CREATE DOMAIN %s.d AS integer; CREATE TABLE %s.t (v %s.d)",
		"enum":                 "CREATE TYPE %s.color AS ENUM ('red','green'); CREATE TABLE %s.t(v %s.color)",
		"custom-type-shadow":   "CREATE DOMAIN %s.integer AS integer; CREATE TABLE %s.t(v %s.integer)",
		"view":                 "CREATE VIEW %s.v AS SELECT 1 AS id",
		"materialized-view":    "CREATE MATERIALIZED VIEW %s.v AS SELECT 1 AS id",
		"sequence":             "CREATE SEQUENCE %s.seq",
		"partition":            "CREATE TABLE %s.t(id integer) PARTITION BY RANGE(id)",
		"unlogged":             "CREATE UNLOGGED TABLE %s.t(id integer)",
		"inheritance":          "CREATE TABLE %s.t(id integer); CREATE TABLE %s.child() INHERITS(%s.t)",
		"check":                "CREATE TABLE %s.t(id integer CHECK(id>0))",
		"deferrable-pk":        "CREATE TABLE %s.t(id integer PRIMARY KEY DEFERRABLE)",
		"custom-pk-name":       "CREATE TABLE %s.t(id integer CONSTRAINT special_pk PRIMARY KEY)",
		"descending-index":     "CREATE TABLE %s.t(id integer); CREATE INDEX t_idx ON %s.t(id DESC)",
		"nulls-first":          "CREATE TABLE %s.t(id integer); CREATE INDEX t_idx ON %s.t(id NULLS FIRST)",
		"partial-index":        "CREATE TABLE %s.t(id integer); CREATE INDEX t_idx ON %s.t(id) WHERE id>0",
		"expression-index":     "CREATE TABLE %s.t(v text); CREATE INDEX t_idx ON %s.t(lower(v))",
		"include-index":        "CREATE TABLE %s.t(id integer,v text); CREATE INDEX t_idx ON %s.t(id) INCLUDE(v)",
		"hash-index":           "CREATE TABLE %s.t(id integer); CREATE INDEX t_idx ON %s.t USING hash(id)",
		"opclass":              "CREATE TABLE %s.t(v text); CREATE INDEX t_idx ON %s.t(v text_pattern_ops)",
		"index-options":        "CREATE TABLE %s.t(id integer); CREATE INDEX t_idx ON %s.t(id) WITH(fillfactor=80)",
		"table-options":        "CREATE TABLE %s.t(id integer) WITH(fillfactor=80)",
		"rls":                  "CREATE TABLE %s.t(id integer); ALTER TABLE %s.t ENABLE ROW LEVEL SECURITY",
		"policy":               "CREATE TABLE %s.t(id integer); CREATE POLICY p ON %s.t USING(true)",
		"rule":                 "CREATE TABLE %s.t(id integer); CREATE RULE nothing AS ON DELETE TO %s.t DO INSTEAD NOTHING",
		"custom-collation":     "CREATE TABLE %s.t(v text COLLATE \"C\")",
		"storage":              "CREATE TABLE %s.t(v text); ALTER TABLE %s.t ALTER COLUMN v SET STORAGE PLAIN",
		"compression":          "CREATE TABLE %s.t(v text COMPRESSION pglz)",
		"quoted-name":          "CREATE TABLE %s.\"MixedCase\"(id integer)",
		"reserved-name":        "CREATE TABLE %s.forgeschema_reserved(id integer)",
		"function":             "CREATE FUNCTION %s.f() RETURNS integer LANGUAGE sql AS 'SELECT 1'",
		"serial":               "CREATE TABLE %s.t (id serial)",
		"function-default":     "CREATE TABLE %s.t (v timestamptz DEFAULT clock_timestamp())",
		"expression-default":   "CREATE TABLE %s.t (v text DEFAULT lower('X'))",
		"timestamp-literal":    "CREATE TABLE %s.t (v timestamptz DEFAULT '2024-01-01')",
		"identity-options":     "CREATE TABLE %s.t (id integer GENERATED ALWAYS AS IDENTITY (START WITH 100))",
		"identity-renamed-seq": "CREATE TABLE %s.t (id integer GENERATED ALWAYS AS IDENTITY); ALTER SEQUENCE %s.t_id_seq RENAME TO other_seq",
		"deferrable-unique":    "CREATE TABLE %s.t(id integer UNIQUE DEFERRABLE)",
		"unique-include":       "CREATE TABLE %s.t(id integer, v text, CONSTRAINT t_key UNIQUE (id) INCLUDE (v))",
		"nulls-not-distinct":   "CREATE TABLE %s.t(id integer UNIQUE NULLS NOT DISTINCT)",
		"unique-index-desc":    "CREATE TABLE %s.t(id integer); CREATE UNIQUE INDEX t_idx ON %s.t(id DESC)",
		"fk-match-full":        "CREATE TABLE %s.t(a integer, b integer, PRIMARY KEY(a,b), pa integer, pb integer, FOREIGN KEY (pa,pb) REFERENCES %s.t(a,b) MATCH FULL)",
		"fk-deferrable":        "CREATE TABLE %s.t(id integer PRIMARY KEY, p integer REFERENCES %s.t(id) DEFERRABLE INITIALLY DEFERRED)",
		"fk-not-valid":         "CREATE TABLE %s.t(id integer PRIMARY KEY, p integer); ALTER TABLE %s.t ADD CONSTRAINT t_p_fkey FOREIGN KEY (p) REFERENCES %s.t(id) NOT VALID",
		"fk-set-null-columns":  "CREATE TABLE %s.t(id integer PRIMARY KEY, a integer, b integer, UNIQUE(id,a), FOREIGN KEY (b,a) REFERENCES %s.t(id,a) ON DELETE SET NULL (a))",
		"extended-statistics":  "CREATE TABLE %s.t(id integer,v text); CREATE STATISTICS %s.st ON id,v FROM %s.t",
	}
	for test, ddl := range cases {
		t.Run(test, func(t *testing.T) {
			db := database(t)
			name := isolated(t, db)
			exec(t, db, strings.ReplaceAll(ddl, "%s", name))
			d, err := postgres.Inspect(context.Background(), db, name)
			if err == nil {
				t.Fatalf("silently accepted unsupported structure: %+v", d)
			}
			var unsupported *postgres.UnsupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("expected unsupported object error, got %v", err)
			}
			if d.Version != 0 {
				t.Fatal("partial snapshot returned on failure")
			}
		})
	}
}
func TestScopeAndDroppedColumns(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	outside := isolated(t, db)
	exec(t, db, "CREATE TABLE "+outside+".other(id serial); CREATE TABLE "+name+".t(id integer, removed text); ALTER TABLE "+name+".t DROP COLUMN removed")
	d, err := postgres.Inspect(context.Background(), db, name)
	if err != nil || len(d.Tables) != 1 || len(d.Tables[0].Columns) != 1 {
		t.Fatalf("scope/dropped column: %+v %v", d, err)
	}
}
func TestReadOnlyRoleAndCancellation(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	exec(t, db, "CREATE TABLE "+name+".t(id integer); INSERT INTO "+name+".t VALUES(1)")
	// No user-table SELECT privilege is required; catalog visibility suffices.
	role := name + "_reader"
	exec(t, db, "CREATE ROLE "+role+" NOLOGIN")
	t.Cleanup(func() {
		_, err := db.Exec("DROP ROLE " + role)
		if err != nil {
			t.Error(err)
		}
	})
	db.SetMaxOpenConns(1)
	exec(t, db, "SET ROLE "+role)
	defer exec(t, db, "RESET ROLE")
	if _, err := postgres.Inspect(context.Background(), db, name); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := postgres.Inspect(ctx, db, name); err == nil {
		t.Fatal("ignored cancellation")
	}
}
