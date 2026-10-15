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
	"sync"
	"testing"
	"time"

	"github.com/ForgeSchema/forgeschema/internal/cli"
	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
)

func applyFixture(t *testing.T, db *sql.DB) (string, plan.Plan) {
	t.Helper()
	name := isolated(t, db)
	fixture, err := os.ReadFile("../testdata/postgres/001-customers.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, db, strings.ReplaceAll(string(fixture), "public.", name+"."))
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := schema.Normalize(src)
	if err != nil {
		t.Fatal(err)
	}
	dst.Tables[0].Columns = append(dst.Tables[0].Columns, schema.Column{Name: "notes", Type: "text", Nullable: true})
	p, err := plan.Build(src, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	return name, p
}
func historyCount(t *testing.T, db *sql.DB, id string) int {
	t.Helper()
	var exists bool
	if err := db.QueryRow("SELECT pg_catalog.to_regclass('forgeschema_meta.migrations') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		return 0
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM forgeschema_meta.migrations WHERE plan_id=$1", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestApplySuccessHistoryAndReplay(t *testing.T) {
	db := database(t)
	name, p := applyFixture(t, db)
	id, _ := p.ID()
	result, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "applied" || result.Operations != 1 || result.PlanID != id || result.MigrationID == "" {
		t.Fatalf("bad result %+v", result)
	}
	actual, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := actual.Fingerprint()
	if fp != p.TargetFingerprint {
		t.Fatal("target mismatch")
	}
	if historyCount(t, db, id) != 1 {
		t.Fatal("missing history")
	}
	var source, target, saved string
	var count int
	if err = db.QueryRow("SELECT source_fingerprint,target_fingerprint,operation_count,plan_json::text FROM forgeschema_meta.migrations WHERE migration_id=$1", result.MigrationID).Scan(&source, &target, &count, &saved); err != nil {
		t.Fatal(err)
	}
	loaded, err := plan.Load(strings.NewReader(saved))
	if err != nil {
		t.Fatal(err)
	}
	savedID, _ := loaded.ID()
	if source != p.SourceFingerprint || target != p.TargetFingerprint || count != 1 || savedID != id {
		t.Fatal("history did not record exact artifact")
	}
	if _, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err == nil {
		t.Fatal("stale replay succeeded")
	}
	if historyCount(t, db, id) != 1 {
		t.Fatal("stale replay changed history")
	}
	var rows int
	if err = db.QueryRow("SELECT count(*) FROM " + name + ".customers WHERE notes IS NULL").Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("data changed: %d %v", rows, err)
	}
}
func TestApplyNoOpAndInvalidPlans(t *testing.T) {
	db := database(t)
	_, p := applyFixture(t, db)
	noOp, err := plan.Build(p.Source, p.Source, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := noOp.ID()
	before := historyCount(t, db, id)
	r, err := postgres.Apply(context.Background(), db, noOp, postgres.ApplyOptions{})
	if err != nil || r.Status != "no_changes" || r.MigrationID != "" || historyCount(t, db, id) != before {
		t.Fatalf("noop: %+v %v", r, err)
	}
	changed := p
	changed.Operations = append([]plan.Operation{}, p.Operations...)
	changed.Operations[0].SQL = "DROP SCHEMA public CASCADE"
	if _, err = postgres.Apply(context.Background(), nil, changed, postgres.ApplyOptions{}); err == nil {
		t.Fatal("tampered plan accepted")
	}
	dst, _ := schema.Normalize(p.Source)
	dst.Tables[0].Columns[0].Type = "integer"
	blocked, err := plan.Build(p.Source, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = postgres.Apply(context.Background(), nil, blocked, postgres.ApplyOptions{}); err == nil {
		t.Fatal("blocked plan accepted")
	}
}
func TestApplyCompetingRuns(t *testing.T) {
	db := database(t)
	_, p := applyFixture(t, db)
	id, _ := p.ID()
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	success, stale := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			var drift *postgres.DriftError
			if !errors.As(err, &drift) || drift.Phase != "source" {
				t.Fatal(err)
			}
			stale++
		}
	}
	if success != 1 || stale != 1 || historyCount(t, db, id) != 1 {
		t.Fatalf("competing runs: successes=%d stale=%d", success, stale)
	}
}
func waitForRelationLock(t *testing.T, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_stat_activity WHERE application_name='forgeschema-apply' AND wait_event_type='Lock' AND query LIKE 'LOCK TABLE ONLY%')`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("apply did not wait for relation lock")
}
func TestApplyFreshDriftCheckAfterLockWait(t *testing.T) {
	db := database(t)
	name, p := applyFixture(t, db)
	id, _ := p.ID()
	external, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer external.Rollback()
	if _, err = external.Exec("ALTER TABLE " + name + ".customers ADD COLUMN external_change text"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); done <- err }()
	waitForRelationLock(t, db)
	if err = external.Commit(); err != nil {
		t.Fatal(err)
	}
	err = <-done
	var drift *postgres.DriftError
	if !errors.As(err, &drift) || drift.Phase != "source" {
		t.Fatalf("missed drift after wait: %v", err)
	}
	if historyCount(t, db, id) != 0 {
		t.Fatal("drift recorded history")
	}
}
func TestApplyLockTimeout(t *testing.T) {
	db := database(t)
	name, p := applyFixture(t, db)
	external, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer external.Rollback()
	if _, err = external.Exec("LOCK TABLE " + name + ".customers IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	_, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{LockTimeout: 100 * time.Millisecond})
	if err == nil {
		t.Fatal("ignored lock timeout")
	}
	if err = external.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err != nil {
		t.Fatal("lock was not released after failed apply:", err)
	}
}
func eventTrigger(t *testing.T, db *sql.DB, body string) {
	t.Helper()
	extra := isolated(t, db)
	trigger := extra + "_ddl"
	exec(t, db, "CREATE FUNCTION "+extra+".ddl_test() RETURNS event_trigger LANGUAGE plpgsql AS $$ BEGIN "+body+" END $$")
	exec(t, db, "CREATE EVENT TRIGGER "+trigger+" ON ddl_command_end WHEN TAG IN ('ALTER TABLE') EXECUTE FUNCTION "+extra+".ddl_test()")
	t.Cleanup(func() {
		_, err := db.Exec("DROP EVENT TRIGGER " + trigger)
		if err != nil {
			t.Error(err)
		}
	})
}
func TestApplyExecutionFailureRollsBackAll(t *testing.T) {
	db := database(t)
	name, p := applyFixture(t, db)
	dst, _ := schema.Normalize(p.Target)
	dst.Tables = append(dst.Tables, schema.Table{Name: "created_first", Columns: []schema.Column{{Name: "id", Type: "integer"}}})
	p, err := plan.Build(p.Source, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := p.ID()
	eventTrigger(t, db, "RAISE EXCEPTION 'intentional test failure';")
	if _, err = postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err == nil {
		t.Fatal("expected SQL failure")
	}
	actual, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := actual.Fingerprint()
	if fp != p.SourceFingerprint || historyCount(t, db, id) != 0 {
		t.Fatal("DDL or history survived failed transaction")
	}
}
func TestApplyTargetVerificationRollsBack(t *testing.T) {
	db := database(t)
	name, p := applyFixture(t, db)
	id, _ := p.ID()
	eventTrigger(t, db, "EXECUTE 'CREATE TABLE "+name+".unexpected(id integer)';")
	_, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{})
	var drift *postgres.DriftError
	if !errors.As(err, &drift) || drift.Phase != "target" {
		t.Fatalf("target mismatch not caught: %v", err)
	}
	actual, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := actual.Fingerprint()
	if fp != p.SourceFingerprint || historyCount(t, db, id) != 0 {
		t.Fatal("target mismatch committed changes")
	}
}
func TestApplyCLI(t *testing.T) {
	db := database(t)
	_, p := applyFixture(t, db)
	b, _ := p.JSON()
	path := t.TempDir() + "/plan.json"
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", os.Getenv("FORGESCHEMA_TEST_DATABASE_URL"))
	var out, errs bytes.Buffer
	if code := cli.Run([]string{"apply", path}, &out, &errs); code != 0 {
		t.Fatalf("apply CLI: %d %s", code, errs.String())
	}
	var result postgres.ApplyResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Status != "applied" {
		t.Fatalf("invalid output %s %v", out.String(), err)
	}
}
func TestApplyRejectsModifiedHistory(t *testing.T) {
	db := database(t)
	_, first := applyFixture(t, db)
	if _, err := postgres.Apply(context.Background(), db, first, postgres.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	name, p := applyFixture(t, db)
	id, _ := p.ID()
	exec(t, db, "ALTER TABLE forgeschema_meta.migrations ADD COLUMN unexpected text")
	t.Cleanup(func() {
		_, err := db.Exec("ALTER TABLE forgeschema_meta.migrations DROP COLUMN unexpected")
		if err != nil {
			t.Error(err)
		}
	})
	if _, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{}); err == nil {
		t.Fatal("accepted modified metadata")
	}
	actual, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := actual.Fingerprint()
	if fp != p.SourceFingerprint || historyCount(t, db, id) != 0 {
		t.Fatal("modified history allowed application DDL")
	}
}
func TestApplyInvalidOptions(t *testing.T) {
	db := database(t)
	_, p := applyFixture(t, db)
	for _, timeout := range []time.Duration{-1, time.Nanosecond} {
		if _, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{LockTimeout: timeout}); err == nil {
			t.Fatal(fmt.Sprintf("accepted invalid timeout %s", timeout))
		}
	}
}
