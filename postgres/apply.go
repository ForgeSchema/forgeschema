package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/schema"
)

// A database-wide transaction advisory lock serializes all ForgeSchema applies,
// including applies targeting different schemas that share history storage.
const coordinationKey int64 = 5066357439090741581
const historySchema = "forgeschema_meta"

// ApplyOptions bounds waiting. Zero values select the defaults: 5s lock
// timeout, 30s statement timeout and a 60s overall deadline.
type ApplyOptions struct{ LockTimeout, StatementTimeout, Timeout time.Duration }
type ApplyResult struct {
	Status            string `json:"status"`
	PlanID            string `json:"plan_id"`
	MigrationID       string `json:"migration_id,omitempty"`
	Operations        int    `json:"operations"`
	TargetFingerprint string `json:"target_fingerprint"`
}
type DriftError struct{ Phase, Expected, Actual string }

func (e *DriftError) Error() string {
	return fmt.Sprintf("%s schema fingerprint mismatch: expected %s, got %s", e.Phase, e.Expected, e.Actual)
}

type ApplyError struct {
	Stage string
	Err   error
	// Resumable means an online plan's first phase is committed: fix the
	// cause and apply the same plan again to finish it.
	Resumable bool
}

func (e *ApplyError) Error() string {
	if e.Resumable {
		return fmt.Sprintf("apply stopped during %s: %v; the first phase is committed, so fix the cause and apply the same plan again to finish", e.Stage, e.Err)
	}
	return fmt.Sprintf("apply failed during %s: %v", e.Stage, e.Err)
}
func (e *ApplyError) Unwrap() error { return e.Err }

// CommitError means the connection did not confirm commit; it must not be
// reported as a known rollback or retried blindly.
type CommitError struct {
	Err                 error
	PlanID, MigrationID string
}

func (e *CommitError) Error() string {
	return "commit outcome is uncertain; inspect schema and migration history before retrying"
}
func (e *CommitError) Unwrap() error { return e.Err }

// Apply executes an exact canonical reviewed plan in one transaction. External
// DDL must be quiescent: relation locks cannot lock a schema namespace against
// unrelated CREATEs. It never replans or executes unvalidated embedded SQL.
func Apply(ctx context.Context, db *sql.DB, p plan.Plan, opts ApplyOptions) (ApplyResult, error) {
	empty := ApplyResult{}
	if err := p.Validate(); err != nil {
		return empty, err
	}
	// Freeze a privately owned validated artifact before waiting on database
	// locks; caller-owned slices must not remain an execution input.
	frozenJSON, err := json.Marshal(p)
	if err != nil {
		return empty, err
	}
	p, err = plan.Load(bytes.NewReader(frozenJSON))
	if err != nil {
		return empty, err
	}
	if p.Status != "ready" {
		return empty, fmt.Errorf("blocked plans cannot be applied")
	}
	if db == nil {
		return empty, fmt.Errorf("database is required")
	}
	if opts.LockTimeout < 0 || opts.StatementTimeout < 0 || opts.Timeout < 0 {
		return empty, fmt.Errorf("timeouts must be positive or zero for defaults")
	}
	if opts.LockTimeout == 0 {
		opts.LockTimeout = 5 * time.Second
	}
	if opts.StatementTimeout == 0 {
		opts.StatementTimeout = 30 * time.Second
	}
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}
	if opts.LockTimeout < time.Millisecond || opts.StatementTimeout < time.Millisecond || opts.Timeout < time.Millisecond {
		return empty, fmt.Errorf("timeouts must be at least one millisecond")
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	id, err := p.ID()
	if err != nil {
		return empty, err
	}
	if _, concurrent, _ := p.Phases(); len(concurrent) > 0 {
		return applyOnline(ctx, db, p, opts, id)
	}
	result := ApplyResult{Status: "no_changes", PlanID: id, TargetFingerprint: p.TargetFingerprint}
	fail := func(stage string, err error) (ApplyResult, error) { return empty, &ApplyError{Stage: stage, Err: err} }
	// The coordination lock is taken on the connection, by polling outside
	// any transaction, before the migration transaction begins; see
	// coordinate.
	conn, err := db.Conn(ctx)
	if err != nil {
		return empty, &ApplyError{Stage: "begin", Err: err}
	}
	defer conn.Close()
	if err = coordinate(ctx, conn, "pg_try_advisory_lock", opts.LockTimeout); err != nil {
		return fail("coordination lock", err)
	}
	defer release(conn)
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return empty, &ApplyError{Stage: "begin", Err: err}
	}
	defer tx.Rollback()
	for _, s := range []struct{ name, value string }{{"search_path", "pg_catalog"}, {"application_name", "forgeschema-apply"}, {"standard_conforming_strings", "on"}, {"lock_timeout", fmt.Sprintf("%dms", opts.LockTimeout.Milliseconds())}, {"statement_timeout", fmt.Sprintf("%dms", opts.StatementTimeout.Milliseconds())}} {
		if _, err = tx.ExecContext(ctx, "SELECT pg_catalog.set_config($1,$2,true)", s.name, s.value); err != nil {
			return fail("configuration", err)
		}
	}
	// Lock only the existing tables the plan changes, plus SHARE ROW EXCLUSIVE
	// on tables referenced by new foreign keys, and hold the locks until
	// commit. Unchanged tables stay readable and writable; structural drift
	// anywhere in the schema is still caught by the fingerprint check below.
	// READ COMMITTED keeps catalog reads fresh after any lock wait.
	exclusive, referenced := p.LockedTables()
	for _, l := range []struct {
		tables []string
		mode   string
	}{{exclusive, "ACCESS EXCLUSIVE"}, {referenced, "SHARE ROW EXCLUSIVE"}} {
		for _, table := range l.tables {
			if _, err = tx.ExecContext(ctx, "LOCK TABLE ONLY "+qualified(p.Source.Schema, table)+" IN "+l.mode+" MODE"); err != nil {
				return fail("relation locks", err)
			}
		}
	}
	live, err := inspectTx(ctx, tx, p.Source.Schema)
	if err != nil {
		return fail("source inspection", err)
	}
	fp, err := live.Fingerprint()
	if err != nil {
		return fail("source fingerprint", err)
	}
	if fp != p.SourceFingerprint {
		return empty, &DriftError{"source", p.SourceFingerprint, fp}
	}
	if len(p.Operations) == 0 {
		if err = tx.Commit(); err != nil {
			return empty, &CommitError{Err: err, PlanID: id}
		}
		return result, nil
	}
	if err = prepareHistory(ctx, tx); err != nil {
		return fail("history validation", err)
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return fail("migration identifier", err)
	}
	result.MigrationID = hex.EncodeToString(token[:])
	for _, op := range p.Operations {
		if _, err = tx.ExecContext(ctx, op.SQL); err != nil {
			return fail("execution", err)
		}
	}
	actual, err := inspectTx(ctx, tx, p.Target.Schema)
	if err != nil {
		return fail("target inspection", err)
	}
	fp, err = actual.Fingerprint()
	if err != nil {
		return fail("target fingerprint", err)
	}
	if fp != p.TargetFingerprint {
		return empty, &DriftError{"target", p.TargetFingerprint, fp}
	}
	artifact, err := json.Marshal(p)
	if err != nil {
		return fail("history encoding", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO forgeschema_meta.migrations
 (migration_id,plan_id,schema_name,source_fingerprint,target_fingerprint,operation_count,applied_at,plan_json)
 VALUES($1,$2,$3,$4,$5,$6,pg_catalog.clock_timestamp(),$7::jsonb)`, result.MigrationID, id, p.Source.Schema, p.SourceFingerprint, p.TargetFingerprint, len(p.Operations), string(artifact))
	if err != nil {
		return fail("history recording", err)
	}
	if err = tx.Commit(); err != nil {
		return empty, &CommitError{Err: err, PlanID: id, MigrationID: result.MigrationID}
	}
	result.Status = "applied"
	result.Operations = len(p.Operations)
	return result, nil
}

// sessionSettings are what every apply sets: catalog-only search_path, its
// name, standard strings and the caller's timeouts.
func sessionSettings(opts ApplyOptions) [][2]string {
	return [][2]string{{"search_path", "pg_catalog"}, {"application_name", "forgeschema-apply"}, {"standard_conforming_strings", "on"},
		{"lock_timeout", fmt.Sprintf("%dms", opts.LockTimeout.Milliseconds())}, {"statement_timeout", fmt.Sprintf("%dms", opts.StatementTimeout.Milliseconds())}}
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// release gives up the coordination lock and session settings before the
// connection goes back to the pool. After a cancelled context database/sql
// may already have discarded the connection, which ends the session and its
// lock on the server, and touching it then can panic inside database/sql.
func release(conn *sql.Conn) {
	defer func() { _ = recover() }()
	_, _ = conn.ExecContext(context.Background(), "SELECT pg_catalog.pg_advisory_unlock_all(); RESET ALL")
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ErrCoordinationBusy means another ForgeSchema apply held the coordination
// lock for longer than the lock timeout.
var ErrCoordinationBusy = errors.New("another ForgeSchema apply is running in this database")

// coordinate takes the session coordination lock by polling with
// autocommit statements outside any transaction. A concurrent index build in
// another apply waits for every transaction that held a snapshot while it
// started, until that transaction ends; a waiter blocked in a transaction,
// or polling inside one, would be such a transaction, and the two would wait
// on each other.
func coordinate(ctx context.Context, q querier, fn string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		var ok bool
		if err := q.QueryRowContext(ctx, "SELECT pg_catalog."+fn+"($1)", coordinationKey).Scan(&ok); err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w (waited %s)", ErrCoordinationBusy, wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func lockTables(ctx context.Context, q execer, schemaName string, exclusive, referenced []string) error {
	for _, l := range []struct {
		tables []string
		mode   string
	}{{exclusive, "ACCESS EXCLUSIVE"}, {referenced, "SHARE ROW EXCLUSIVE"}} {
		for _, table := range l.tables {
			if _, err := q.ExecContext(ctx, "LOCK TABLE ONLY "+qualified(schemaName, table)+" IN "+l.mode+" MODE"); err != nil {
				return err
			}
		}
	}
	return nil
}

func fingerprint(d schema.Document) string {
	fp, _ := d.Fingerprint()
	return fp
}

// applyOnline runs a plan with concurrent index builds in three phases on one
// connection, which holds the session-level coordination lock throughout:
//
//  1. a READ COMMITTED transaction for the operations before the builds,
//     checked against the plan's first intermediate schema, then committed;
//  2. each CREATE [UNIQUE] INDEX CONCURRENTLY on its own, outside any
//     transaction;
//  3. a transaction that attaches unique constraints, adds foreign keys,
//     checks the full target and records history.
//
// If phase 2 or 3 fails, phase 1 stays committed and no history is written.
// Applying the same plan again resumes: it recognises the first intermediate
// schema, plus any indexes already built, drops an invalid index left by a
// failed build and builds it again.
func applyOnline(ctx context.Context, db *sql.DB, p plan.Plan, opts ApplyOptions, id string) (ApplyResult, error) {
	empty := ApplyResult{}
	first, concurrent, final := p.Phases()
	afterFirst, beforeFinal, err := p.Intermediate()
	if err != nil {
		return empty, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return empty, &ApplyError{Stage: "begin", Err: err}
	}
	defer func() {
		// The connection goes back to the pool: release the session lock and
		// settings. A broken connection is discarded by the driver anyway.
		release(conn)
		conn.Close()
	}()
	committed := false
	fail := func(stage string, err error) (ApplyResult, error) {
		return empty, &ApplyError{Stage: stage, Err: err, Resumable: committed}
	}
	for _, kv := range sessionSettings(opts) {
		if _, err = conn.ExecContext(ctx, "SELECT pg_catalog.set_config($1,$2,false)", kv[0], kv[1]); err != nil {
			return fail("configuration", err)
		}
	}
	if err = coordinate(ctx, conn, "pg_try_advisory_lock", opts.LockTimeout); err != nil {
		return fail("coordination lock", err)
	}
	built := map[string]bool{}
	for _, op := range concurrent {
		built[op.Subject] = true
	}

	// Phase 1, or recognising that it ran before.
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fail("begin", err)
	}
	defer tx.Rollback()
	exclusive, referenced := p.LockedTablesFor(first)
	if err = lockTables(ctx, tx, p.Source.Schema, exclusive, referenced); err != nil {
		return fail("relation locks", err)
	}
	live, invalid, err := inspectTxAllowing(ctx, tx, p.Source.Schema, built)
	if err != nil {
		return fail("source inspection", err)
	}
	if err = prepareHistory(ctx, tx); err != nil {
		return fail("history validation", err)
	}
	if fingerprint(live) == p.SourceFingerprint && len(invalid) == 0 {
		for _, op := range first {
			if _, err = tx.ExecContext(ctx, op.SQL); err != nil {
				return fail("execution", err)
			}
		}
		after, err := inspectTx(ctx, tx, p.Source.Schema)
		if err != nil {
			return fail("intermediate inspection", err)
		}
		if got := fingerprint(after); got != fingerprint(afterFirst) {
			return empty, &DriftError{"intermediate", fingerprint(afterFirst), got}
		}
		if err = tx.Commit(); err != nil {
			return empty, &CommitError{Err: err, PlanID: id}
		}
	} else {
		resumed, err := withoutBuilt(live, beforeFinal, built)
		if err != nil {
			return empty, &DriftError{"source", p.SourceFingerprint, fingerprint(live)}
		}
		if fingerprint(resumed) != fingerprint(afterFirst) {
			return empty, &DriftError{"source", p.SourceFingerprint, fingerprint(live)}
		}
		// A finished plan that only builds indexes looks like one to resume:
		// its history row tells them apart, so it is never applied twice.
		var done bool
		if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM forgeschema_meta.migrations WHERE plan_id=$1)", id).Scan(&done); err != nil {
			return fail("history validation", err)
		}
		if done {
			return empty, &DriftError{"source", p.SourceFingerprint, fingerprint(live)}
		}
		if err = tx.Rollback(); err != nil {
			return fail("resume", err)
		}
	}
	committed = true

	// Phase 2: each build on its own, outside a transaction. A build waits
	// for every older transaction in the database, and that wait counts as a
	// lock wait, so lock_timeout is off here; statement_timeout and the
	// overall deadline still bound it.
	if _, err = conn.ExecContext(ctx, "SELECT pg_catalog.set_config('lock_timeout','0',false)"); err != nil {
		return fail("configuration", err)
	}
	for _, op := range concurrent {
		stage := "concurrent index " + op.Subject
		var valid sql.NullBool
		err := conn.QueryRowContext(ctx, `SELECT i.indisvalid AND i.indisready AND i.indislive FROM pg_catalog.pg_class c
 JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace JOIN pg_catalog.pg_index i ON i.indexrelid=c.oid
 JOIN pg_catalog.pg_class t ON t.oid=i.indrelid
 WHERE n.nspname=$1 AND c.relname=$2 AND t.relname=$3`, p.Source.Schema, op.Subject, op.Table).Scan(&valid)
		switch {
		case err == sql.ErrNoRows:
		case err != nil:
			return fail(stage, err)
		case valid.Bool:
			continue // built by an earlier run; its definition was checked above
		default:
			if _, err = conn.ExecContext(ctx, "DROP INDEX CONCURRENTLY "+qualified(p.Source.Schema, op.Subject)); err != nil {
				return fail(stage, err)
			}
		}
		if _, err = conn.ExecContext(ctx, op.SQL); err != nil {
			return fail(stage, err)
		}
	}

	// Phase 3: attach constraints, add foreign keys, verify, record history.
	if _, err = conn.ExecContext(ctx, "SELECT pg_catalog.set_config('lock_timeout',$1,false)", fmt.Sprintf("%dms", opts.LockTimeout.Milliseconds())); err != nil {
		return fail("configuration", err)
	}
	tx, err = conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fail("begin", err)
	}
	defer tx.Rollback()
	exclusive, referenced = p.LockedTablesFor(final)
	if err = lockTables(ctx, tx, p.Source.Schema, exclusive, referenced); err != nil {
		return fail("relation locks", err)
	}
	live, err = inspectTx(ctx, tx, p.Source.Schema)
	if err != nil {
		return fail("intermediate inspection", err)
	}
	if got := fingerprint(live); got != fingerprint(beforeFinal) {
		return empty, &DriftError{"final", fingerprint(beforeFinal), got}
	}
	if err = prepareHistory(ctx, tx); err != nil {
		return fail("history validation", err)
	}
	for _, op := range final {
		if _, err = tx.ExecContext(ctx, op.SQL); err != nil {
			return fail("execution", err)
		}
	}
	actual, err := inspectTx(ctx, tx, p.Target.Schema)
	if err != nil {
		return fail("target inspection", err)
	}
	if got := fingerprint(actual); got != p.TargetFingerprint {
		return empty, &DriftError{"target", p.TargetFingerprint, got}
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return fail("migration identifier", err)
	}
	result := ApplyResult{Status: "applied", PlanID: id, MigrationID: hex.EncodeToString(token[:]), Operations: len(p.Operations), TargetFingerprint: p.TargetFingerprint}
	artifact, err := json.Marshal(p)
	if err != nil {
		return fail("history encoding", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO forgeschema_meta.migrations
 (migration_id,plan_id,schema_name,source_fingerprint,target_fingerprint,operation_count,applied_at,plan_json)
 VALUES($1,$2,$3,$4,$5,$6,pg_catalog.clock_timestamp(),$7::jsonb)`, result.MigrationID, id, p.Source.Schema, p.SourceFingerprint, p.TargetFingerprint, len(p.Operations), string(artifact)); err != nil {
		return fail("history recording", err)
	}
	if err = tx.Commit(); err != nil {
		return empty, &CommitError{Err: err, PlanID: id, MigrationID: result.MigrationID}
	}
	return result, nil
}

// withoutBuilt removes from live the indexes the plan builds concurrently,
// after checking each one present matches its definition in beforeFinal.
func withoutBuilt(live, beforeFinal schema.Document, built map[string]bool) (schema.Document, error) {
	want := map[string]schema.Index{}
	for _, t := range beforeFinal.Tables {
		for _, x := range t.Indexes {
			if built[x.Name] {
				want[t.Name+"\x00"+x.Name] = x
			}
		}
	}
	d, err := schema.Normalize(live)
	if err != nil {
		return d, err
	}
	for ti := range d.Tables {
		t := &d.Tables[ti]
		var keep []schema.Index
		for _, x := range t.Indexes {
			if !built[x.Name] {
				keep = append(keep, x)
				continue
			}
			w, ok := want[t.Name+"\x00"+x.Name]
			if !ok || w.Unique != x.Unique || strings.Join(w.Columns, "\x00") != strings.Join(x.Columns, "\x00") {
				return d, fmt.Errorf("index %s.%s differs from the plan", t.Name, x.Name)
			}
		}
		t.Indexes = keep
	}
	return schema.Normalize(d)
}

func prepareHistory(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS forgeschema_meta"); err != nil {
		return err
	}
	var oid int64
	var trusted bool
	err := tx.QueryRowContext(ctx, `SELECT n.oid,n.nspowner=(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=current_user)
 AND NOT EXISTS(SELECT 1 FROM pg_catalog.aclexplode(COALESCE(n.nspacl,pg_catalog.acldefault('n',n.nspowner))) a WHERE a.grantee<>n.nspowner)
 FROM pg_catalog.pg_namespace n WHERE n.nspname=$1`, historySchema).Scan(&oid, &trusted)
	if err != nil {
		return err
	}
	if !trusted {
		return fmt.Errorf("history schema must be owned exclusively by the applying role")
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS forgeschema_meta.migrations (
 migration_id text NOT NULL PRIMARY KEY,plan_id text NOT NULL,schema_name text NOT NULL,
 source_fingerprint text NOT NULL,target_fingerprint text NOT NULL,operation_count integer NOT NULL,
 applied_at timestamp with time zone NOT NULL,plan_json jsonb NOT NULL)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "LOCK TABLE ONLY forgeschema_meta.migrations IN ACCESS EXCLUSIVE MODE"); err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT c.relowner=(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=current_user)
 AND NOT EXISTS(SELECT 1 FROM pg_catalog.aclexplode(COALESCE(c.relacl,pg_catalog.acldefault('r',c.relowner))) a WHERE a.grantee<>c.relowner)
 FROM pg_catalog.pg_class c WHERE c.relnamespace=$1 AND c.relname='migrations'`, oid).Scan(&trusted)
	if err != nil {
		return err
	}
	if !trusted {
		return fmt.Errorf("history table must be owned exclusively by the applying role")
	}
	if err = checkScope(ctx, tx, oid, historySchema); err != nil {
		return err
	}
	actual, err := readTables(ctx, tx, oid, historySchema)
	if err != nil {
		return err
	}
	// Inspect the internal schema explicitly without weakening managed-schema
	// validation. Only this exact table is accepted; no silent object exclusions.
	actual.Schema = "metadata"
	expected := schema.Document{Version: 1, Schema: "metadata", Tables: []schema.Table{{Name: "migrations", Columns: []schema.Column{
		{Name: "migration_id", Type: "text"}, {Name: "plan_id", Type: "text"}, {Name: "schema_name", Type: "text"},
		{Name: "source_fingerprint", Type: "text"}, {Name: "target_fingerprint", Type: "text"}, {Name: "operation_count", Type: "integer"},
		{Name: "applied_at", Type: "timestamp with time zone"}, {Name: "plan_json", Type: "jsonb"},
	}, PrimaryKey: []string{"migration_id"}}}}
	got, err := actual.CanonicalJSON()
	if err != nil {
		return err
	}
	want, err := expected.CanonicalJSON()
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("history schema definition differs from the expected engine metadata")
	}
	return nil
}
func qualified(s, t string) string { return quoteName(s) + "." + quoteName(t) }
func quoteName(s string) string    { return `"` + s + `"` } // Names have already passed schema normalization.
