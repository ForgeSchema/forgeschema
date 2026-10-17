package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ForgeSchema/forgeschema/internal/cli"
	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
)

func TestVerifyReadOnlyRoleAndDrift(t *testing.T) {
	db := database(t)
	name, p := applyFixture(t, db)
	db.SetMaxOpenConns(1)
	role := name + "_verify"
	exec(t, db, "CREATE ROLE "+role+" NOLOGIN")
	t.Cleanup(func() {
		_, err := db.Exec("DROP ROLE " + role)
		if err != nil {
			t.Error(err)
		}
	})
	exec(t, db, "SET ROLE "+role)
	defer exec(t, db, "RESET ROLE")
	exec(t, db, "SET default_transaction_read_only=on")
	defer exec(t, db, "RESET default_transaction_read_only")
	match, err := postgres.Verify(context.Background(), db, p.Source)
	if err != nil || match.Status != "match" || len(match.Differences) != 0 || match.Differences == nil || match.ActualFingerprint != match.ExpectedFingerprint {
		t.Fatalf("match: %+v %v", match, err)
	}
	drift, err := postgres.Verify(context.Background(), db, p.Target)
	if err != nil || drift.Status != "drift" || len(drift.Differences) != 1 || drift.Differences[0].Kind != "column_missing" {
		t.Fatalf("drift: %+v %v", drift, err)
	}
}
func TestVerifyUnsupportedAndCancelled(t *testing.T) {
	db := database(t)
	name, p := applyFixture(t, db)
	exec(t, db, "ALTER TABLE "+name+".customers ALTER COLUMN email SET DEFAULT lower('UNSUPPORTED')")
	result, err := postgres.Verify(context.Background(), db, p.Source)
	var unsupported *postgres.UnsupportedError
	if !errors.As(err, &unsupported) || result.Status != "" {
		t.Fatalf("unsupported structure yielded report: %+v %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = postgres.Verify(ctx, db, p.Source); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err = postgres.Verify(context.Background(), nil, schema.Document{}); err == nil {
		t.Fatal("accepted invalid expected schema")
	}
}
func cliOutput(t *testing.T, args []string, want int) []byte {
	t.Helper()
	var out, errs bytes.Buffer
	code := cli.Run(args, &out, &errs)
	if code != want {
		t.Fatalf("%v: exit %d want %d; %s", args, code, want, errs.String())
	}
	return append([]byte{}, out.Bytes()...)
}
func writeTestFile(t *testing.T, name string, b []byte) string {
	t.Helper()
	path := t.TempDir() + "/" + name
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestFullCLIWorkflowAndVerification(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	fixture, err := os.ReadFile("../testdata/postgres/001-customers.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, db, strings.ReplaceAll(string(fixture), "public.", name+"."))
	exec(t, db, "CREATE INDEX customers_name_idx ON "+name+".customers(customer_name,id)")
	t.Setenv("DATABASE_URL", os.Getenv("FORGESCHEMA_TEST_DATABASE_URL"))
	snapshot := cliOutput(t, []string{"inspect", name}, 0)
	sourcePath := writeTestFile(t, "current.json", snapshot)
	src, err := schema.Load(bytes.NewReader(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	dst, err := schema.Normalize(src)
	if err != nil {
		t.Fatal(err)
	}
	for i := range dst.Tables[0].Columns {
		switch dst.Tables[0].Columns[i].Name {
		case "id":
			dst.Tables[0].Columns[i].Name = "customer_id"
		case "customer_name":
			dst.Tables[0].Columns[i].Name = "full_name"
		}
	}
	dst.Tables[0].PrimaryKey = []string{"customer_id"}
	for i := range dst.Tables[0].Indexes {
		if dst.Tables[0].Indexes[i].Name == "customers_name_idx" {
			dst.Tables[0].Indexes[i].Columns = []string{"full_name", "customer_id"}
		}
	}
	dst.Tables[0].Columns = append(dst.Tables[0].Columns, schema.Column{Name: "notes", Type: "text", Nullable: true})
	dst.Tables[0].Indexes = append(dst.Tables[0].Indexes, schema.Index{Name: "customers_notes_idx", Columns: []string{"notes"}})
	dst.Tables = append(dst.Tables, schema.Table{Name: "select", Columns: []schema.Column{{Name: "from", Type: "integer"}}, PrimaryKey: []string{"from"}, Indexes: []schema.Index{{Name: "select_from_idx", Columns: []string{"from"}}}})
	b, err := dst.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	targetPath := writeTestFile(t, "desired.json", b)
	renames := writeTestFile(t, "renames.yaml", []byte("version: 1\ncolumns:\n- table: customers\n  from: id\n  to: customer_id\n- table: customers\n  from: customer_name\n  to: full_name\n"))
	artifact := cliOutput(t, []string{"plan", sourcePath, targetPath, renames}, 0)
	p, err := plan.Load(bytes.NewReader(artifact))
	if err != nil || len(p.Operations) != 6 {
		t.Fatalf("full plan: %+v %v", p, err)
	}
	path := writeTestFile(t, "reviewed-plan.json", artifact)
	before := cliOutput(t, []string{"verify", "--plan", path}, 4)
	var report postgres.VerifyResult
	if err = json.Unmarshal(before, &report); err != nil || report.Status != "drift" {
		t.Fatalf("before apply: %s %v", before, err)
	}
	cliOutput(t, []string{"apply", path}, 0)
	for _, args := range [][]string{{"verify", targetPath}, {"verify", "--plan", path}} {
		raw := cliOutput(t, args, 0)
		if err = json.Unmarshal(raw, &report); err != nil || report.Status != "match" || len(report.Differences) != 0 {
			t.Fatalf("post-apply: %s %v", raw, err)
		}
	}
	id, _ := p.ID()
	if historyCount(t, db, id) != 1 {
		t.Fatal("full migration missing history")
	}
	var n int
	if err = db.QueryRow("SELECT count(*) FROM " + name + ".customers WHERE (customer_id=1 AND full_name='Ada Example' AND email='ada@example.invalid' OR customer_id=2 AND full_name='Dana Example' AND email IS NULL) AND notes IS NULL").Scan(&n); err != nil || n != 2 {
		t.Fatalf("original data did not survive full CLI flow: %d %v", n, err)
	}
	exec(t, db, "ALTER TABLE "+name+".customers ADD COLUMN unexpected text")
	raw := cliOutput(t, []string{"verify", "--plan", path}, 4)
	if err = json.Unmarshal(raw, &report); err != nil || len(report.Differences) != 1 || report.Differences[0].Kind != "column_unexpected" {
		t.Fatalf("later drift: %s %v", raw, err)
	}
	if historyCount(t, db, id) != 1 {
		t.Fatal("read-only verify modified history")
	}
	t.Log("Full inspect → plan → apply → verify workflow passed: six operations, preserved rows, later drift detected without repair")
}
func TestVerifyRejectsBlockedPlan(t *testing.T) {
	db := database(t)
	_, p := applyFixture(t, db)
	dst, _ := schema.Normalize(p.Source)
	dst.Tables[0].Columns[0].Type = "integer"
	blocked, err := plan.Build(p.Source, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := blocked.JSON()
	path := writeTestFile(t, "blocked.json", b)
	t.Setenv("DATABASE_URL", "")
	var out, errs bytes.Buffer
	if code := cli.Run([]string{"verify", "--plan", path}, &out, &errs); code != 3 || out.Len() != 0 {
		t.Fatalf("blocked verify %d %s", code, errs.String())
	}
}
func TestApplyCancellationReleasesLocks(t *testing.T) {
	db := database(t)
	name, p := applyFixture(t, db)
	id, _ := p.ID()
	external, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer external.Rollback()
	if _, err = external.Exec("LOCK TABLE " + name + ".customers IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := postgres.Apply(ctx, db, p, postgres.ApplyOptions{}); done <- err }()
	waitForRelationLock(t, db)
	cancel()
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("cancelled apply succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled apply did not stop")
	}
	if err = external.Rollback(); err != nil {
		t.Fatal(err)
	}
	actual, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := actual.Fingerprint()
	if fp != p.SourceFingerprint || historyCount(t, db, id) != 0 {
		t.Fatal("cancellation changed schema/history")
	}
	if _, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal("cancellation leaked migration locks:", err)
	}
}
func TestApplyStatementTimeoutRollsBack(t *testing.T) {
	db := database(t)
	name, p := applyFixture(t, db)
	id, _ := p.ID()
	eventTrigger(t, db, "PERFORM pg_catalog.pg_sleep(2);")
	_, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{StatementTimeout: 200 * time.Millisecond})
	var stage *postgres.ApplyError
	if !errors.As(err, &stage) || stage.Stage != "execution" {
		t.Fatalf("expected statement timeout during execution: %v", err)
	}
	actual, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := actual.Fingerprint()
	if fp != p.SourceFingerprint || historyCount(t, db, id) != 0 {
		t.Fatal("timeout did not roll back")
	}
}

// These tests use the documented disposable database and are deliberately not
// parallel: metadata collision scenarios temporarily isolate global history.
func isolateMetadata(t *testing.T, db *sql.DB) {
	t.Helper()
	backup := fmt.Sprintf("fs_meta_backup_%d", time.Now().UnixNano())
	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname='forgeschema_meta')").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		exec(t, db, "ALTER SCHEMA forgeschema_meta RENAME TO "+backup)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("RESET ROLE"); err != nil {
			t.Error(err)
		}
		if _, err := db.Exec("DROP SCHEMA IF EXISTS forgeschema_meta CASCADE"); err != nil {
			t.Error(err)
		}
		if exists {
			if _, err := db.Exec("ALTER SCHEMA " + backup + " RENAME TO forgeschema_meta"); err != nil {
				t.Error(err)
			}
		}
	})
}
func TestApplyMetadataBootstrapFailureIsAtomic(t *testing.T) {
	db := database(t)
	name, p := applyFixture(t, db)
	isolateMetadata(t, db)
	exec(t, db, "CREATE SCHEMA forgeschema_meta; CREATE TABLE forgeschema_meta.intruder(id integer)")
	_, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{})
	var stage *postgres.ApplyError
	if !errors.As(err, &stage) || stage.Stage != "history validation" {
		t.Fatalf("unexpected bootstrap result %v", err)
	}
	var migrations, original bool
	if err = db.QueryRow("SELECT to_regclass('forgeschema_meta.migrations') IS NOT NULL,to_regclass('forgeschema_meta.intruder') IS NOT NULL").Scan(&migrations, &original); err != nil || migrations || !original {
		t.Fatalf("bootstrap did not roll back only its own creation: %t %t %v", migrations, original, err)
	}
	actual, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := actual.Fingerprint()
	if fp != p.SourceFingerprint {
		t.Fatal("metadata failure changed application schema")
	}
}
func TestApplyHistoryInsertFailureRollsBackDDL(t *testing.T) {
	db := database(t)
	db.SetMaxOpenConns(1)
	isolateMetadata(t, db)
	role := fmt.Sprintf("fs_history_role_%d", time.Now().UnixNano())
	exec(t, db, "CREATE ROLE "+role+" NOLOGIN")
	var databaseName string
	if err := db.QueryRow("SELECT current_database()").Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	exec(t, db, "GRANT CREATE ON DATABASE \""+strings.ReplaceAll(databaseName, "\"", "\"\"")+"\" TO "+role)
	t.Cleanup(func() {
		for _, q := range []string{"RESET ROLE", "DROP OWNED BY " + role + " CASCADE", "DROP ROLE " + role} {
			if _, err := db.Exec(q); err != nil {
				t.Error(err)
			}
		}
	})
	exec(t, db, "SET ROLE "+role)
	defer exec(t, db, "RESET ROLE")
	name, first := applyFixture(t, db)
	if _, err := postgres.Apply(context.Background(), db, first, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	dst, _ := schema.Normalize(src)
	dst.Tables[0].Columns = append(dst.Tables[0].Columns, schema.Column{Name: "another", Type: "text", Nullable: true})
	p, err := plan.Build(src, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := p.ID()
	exec(t, db, "REVOKE INSERT ON forgeschema_meta.migrations FROM "+role)
	_, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{})
	var stage *postgres.ApplyError
	if !errors.As(err, &stage) || stage.Stage != "history recording" {
		t.Fatalf("expected history INSERT denial, got %v", err)
	}
	actual, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := actual.Fingerprint()
	if fp != p.SourceFingerprint || historyCount(t, db, id) != 0 {
		t.Fatal("history INSERT failure committed application DDL")
	}
}
