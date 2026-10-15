package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/schema"
)

// A database-wide transaction advisory lock serializes all ForgeSchema applies,
// including applies targeting different schemas that share history storage.
const coordinationKey int64 = 5066357439090741581
const historySchema = "forgeschema_meta"

type ApplyOptions struct{ LockTimeout, StatementTimeout time.Duration }
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
}

func (e *ApplyError) Error() string { return fmt.Sprintf("apply failed during %s: %v", e.Stage, e.Err) }
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
	if p.Status != "ready" {
		return empty, fmt.Errorf("blocked plans cannot be applied")
	}
	if db == nil {
		return empty, fmt.Errorf("database is required")
	}
	if opts.LockTimeout < 0 || opts.StatementTimeout < 0 {
		return empty, fmt.Errorf("timeouts must be positive or zero for defaults")
	}
	if opts.LockTimeout == 0 {
		opts.LockTimeout = 5 * time.Second
	}
	if opts.StatementTimeout == 0 {
		opts.StatementTimeout = 30 * time.Second
	}
	if opts.LockTimeout < time.Millisecond || opts.StatementTimeout < time.Millisecond {
		return empty, fmt.Errorf("timeouts must be at least one millisecond")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	id, err := p.ID()
	if err != nil {
		return empty, err
	}
	result := ApplyResult{Status: "no_changes", PlanID: id, TargetFingerprint: p.TargetFingerprint}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return empty, &ApplyError{"begin", err}
	}
	defer tx.Rollback()
	fail := func(stage string, err error) (ApplyResult, error) { return empty, &ApplyError{stage, err} }
	for _, s := range []struct{ name, value string }{{"search_path", "pg_catalog"}, {"application_name", "forgeschema-apply"}, {"lock_timeout", fmt.Sprintf("%dms", opts.LockTimeout.Milliseconds())}, {"statement_timeout", fmt.Sprintf("%dms", opts.StatementTimeout.Milliseconds())}} {
		if _, err = tx.ExecContext(ctx, "SELECT pg_catalog.set_config($1,$2,true)", s.name, s.value); err != nil {
			return fail("configuration", err)
		}
	}
	if _, err = tx.ExecContext(ctx, "SELECT pg_catalog.pg_advisory_xact_lock($1)", coordinationKey); err != nil {
		return fail("coordination lock", err)
	}
	// Hold locks until commit. READ COMMITTED ensures the subsequent catalog
	// reads are fresh even if acquiring one of these locks had to wait.
	for _, table := range p.Source.Tables {
		if _, err = tx.ExecContext(ctx, "LOCK TABLE ONLY "+qualified(p.Source.Schema, table.Name)+" IN ACCESS EXCLUSIVE MODE"); err != nil {
			return fail("relation locks", err)
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
