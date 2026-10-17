// Package demo implements the isolated disposable-database walkthrough.
// It is a demonstration harness, not part of the production migration API.
package demo

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
	_ "github.com/lib/pq"
)

var artifactNames = []string{"run.json", "current.json", "desired.json", "renames.yaml", "reviewed-plan.json", "review.txt", "before-rows.json", "after-rows.json", "apply-result.json", "verify-match.json", "replay-error.txt", "before-drift-check.json", "verify-drift.json", "after-drift-check.json", "verify-final.json", "transcript.txt"}

type options struct {
	binary, output string
	keep, bundle   bool
}
type report struct {
	Status            string `json:"status"`
	Schema            string `json:"schema"`
	PostgreSQLVersion string `json:"postgresql_version"`
	CLIVersion        string `json:"cli_version"`
	Operations        int    `json:"operations"`
	RowsPreserved     int    `json:"rows_preserved"`
	PlanID            string `json:"plan_id"`
	MigrationID       string `json:"migration_id"`
	DriftExit         int    `json:"drift_exit"`
	ReplayExit        int    `json:"replay_exit"`
	Cleanup           string `json:"cleanup"`
}
type row struct {
	ID    int64   `json:"id"`
	Name  string  `json:"name"`
	Email *string `json:"email"`
}

func Run(ctx context.Context, args []string, out, errs io.Writer) int {
	o := options{}
	fs := flag.NewFlagSet("forgeschema-demo", flag.ContinueOnError)
	fs.SetOutput(errs)
	fs.StringVar(&o.binary, "binary", "./bin/forgeschema", "compiled ForgeSchema CLI path")
	fs.StringVar(&o.output, "output", "demo-output", "new artifact directory; existing directories are never overwritten")
	fs.BoolVar(&o.keep, "keep-schema", false, "keep the new isolated demo schema for manual inspection")
	fs.BoolVar(&o.bundle, "emit-bundle", false, "emit synthetic walkthrough artifacts as base64 records for CI evidence")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(errs, "forgeschema-demo: unexpected arguments")
		return 2
	}
	dsn := os.Getenv("FORGESCHEMA_DEMO_DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(errs, "forgeschema-demo: explicitly set FORGESCHEMA_DEMO_DATABASE_URL to a disposable PostgreSQL 16/17 database")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := execute(ctx, o, dsn, out); err != nil {
		fmt.Fprintln(errs, "forgeschema-demo:", err)
		return 1
	}
	return 0
}
func execute(ctx context.Context, o options, dsn string, out io.Writer) (err error) {
	binary, err := filepath.Abs(o.binary)
	if err != nil {
		return err
	}
	if info, e := os.Stat(binary); e != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("build the ForgeSchema CLI before running the walkthrough")
	}
	if err = os.MkdirAll(filepath.Dir(o.output), 0700); err != nil {
		return err
	}
	if err = os.Mkdir(o.output, 0700); err != nil {
		return fmt.Errorf("create a NEW output directory: %w", err)
	}
	var transcript bytes.Buffer
	log := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		fmt.Fprintln(out, line)
		fmt.Fprintln(&transcript, line)
	}
	r := report{Status: "failed", Cleanup: "not_created"}
	var db *sql.DB
	created := false
	write := func(name string, b []byte) error { return os.WriteFile(filepath.Join(o.output, name), b, 0600) }
	defer func() {
		if created {
			if o.keep {
				r.Cleanup = "kept"
				log("Kept isolated schema %s; final target is restored after the drift demonstration.", r.Schema)
			} else {
				cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				_, cleanupErr := db.ExecContext(cleanup, `DROP SCHEMA `+r.Schema+` CASCADE`)
				cancel()
				if cleanupErr != nil {
					r.Cleanup = "failed"
					log("Cleanup failed; isolated schema %s may remain.", r.Schema)
					if err == nil {
						err = fmt.Errorf("cleanup of isolated schema failed")
					}
				} else {
					r.Cleanup = "dropped"
					log("Dropped only isolated demo schema %s; migration history is retained.", r.Schema)
				}
			}
		}
		if db != nil {
			db.Close()
		}
		if err == nil {
			r.Status = "passed"
		}
		encoded, e := json.MarshalIndent(r, "", "  ")
		if e == nil {
			e = write("run.json", append(encoded, '\n'))
		}
		if e != nil && err == nil {
			err = e
		}
		if e = write("transcript.txt", transcript.Bytes()); e != nil && err == nil {
			err = e
		}
		if err == nil && o.bundle {
			for _, name := range artifactNames {
				b, e := os.ReadFile(filepath.Join(o.output, name))
				if e != nil {
					err = e
					return
				}
				fmt.Fprintf(out, "FORGESCHEMA_DEMO_FILE %s %s\n", name, base64.StdEncoding.EncodeToString(b))
			}
		}
	}()
	childEnv := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "DATABASE_URL=") {
			childEnv = append(childEnv, e)
		}
	}
	childEnv = append(childEnv, "DATABASE_URL="+dsn)
	cli := func(want int, file string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = childEnv
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		e := cmd.Run()
		code := 0
		if e != nil {
			var exit *exec.ExitError
			if errors.As(e, &exit) {
				code = exit.ExitCode()
			} else {
				return nil, fmt.Errorf("could not run compiled CLI")
			}
		}
		if code != want {
			return nil, fmt.Errorf("%s exited %d (expected %d): %s", args[0], code, want, stderr.String())
		}
		body := stdout.Bytes()
		if file == "replay-error.txt" {
			body = stderr.Bytes()
		}
		if file != "" {
			if e = write(file, body); e != nil {
				return nil, e
			}
		}
		log("%s completed with expected exit %d%s", strings.Join(args, " "), code, func() string {
			if file != "" {
				return " → " + file
			}
			return ""
		}())
		return append([]byte{}, body...), nil
	}
	version, err := cli(0, "", "version")
	if err != nil {
		return err
	}
	r.CLIVersion = strings.TrimSpace(string(version))
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("invalid disposable database configuration")
	}
	if err = db.PingContext(ctx); err != nil {
		return fmt.Errorf("disposable database connection failed; check FORGESCHEMA_DEMO_DATABASE_URL")
	}
	if err = db.QueryRowContext(ctx, "SHOW server_version").Scan(&r.PostgreSQLVersion); err != nil {
		return err
	}
	var token [8]byte
	if _, err = rand.Read(token[:]); err != nil {
		return err
	}
	r.Schema = "fs_demo_" + hex.EncodeToString(token[:])
	log("ForgeSchema walkthrough: CLI %s; PostgreSQL %s; isolated schema %s", r.CLIVersion, r.PostgreSQLVersion, r.Schema)
	if _, err = db.ExecContext(ctx, "CREATE SCHEMA "+r.Schema); err != nil {
		return fmt.Errorf("could not create the new isolated demo schema")
	}
	created = true
	fixture, err := os.ReadFile("testdata/postgres/001-customers.sql")
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, strings.ReplaceAll(string(fixture), "public.", r.Schema+".")); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "CREATE INDEX customers_name_idx ON "+r.Schema+".customers(customer_name,id)"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("fixture commit was not confirmed")
	}
	rows := func(after bool) ([]row, error) {
		fields := "id,customer_name,email"
		if after {
			fields = "customer_id,full_name,email"
		}
		rs, e := db.QueryContext(ctx, "SELECT "+fields+" FROM "+r.Schema+".customers ORDER BY 1")
		if e != nil {
			return nil, e
		}
		defer rs.Close()
		data := []row{}
		for rs.Next() {
			var item row
			var email sql.NullString
			if e = rs.Scan(&item.ID, &item.Name, &email); e != nil {
				return nil, e
			}
			if email.Valid {
				v := email.String
				item.Email = &v
			}
			data = append(data, item)
		}
		return data, rs.Err()
	}
	before, err := rows(false)
	if err != nil {
		return err
	}
	if len(before) != 2 || before[0].ID != 1 || before[0].Name != "Ada Example" || before[1].ID != 2 || before[1].Name != "Dana Example" {
		return fmt.Errorf("walkthrough fixture does not match the documented synthetic customers")
	}
	beforeJSON, _ := json.MarshalIndent(before, "", "  ")
	if err = write("before-rows.json", append(beforeJSON, '\n')); err != nil {
		return err
	}
	current, err := cli(0, "current.json", "inspect", r.Schema)
	if err != nil {
		return err
	}
	src, err := schema.Load(bytes.NewReader(current))
	if err != nil {
		return err
	}
	target, err := schema.Normalize(src)
	if err != nil {
		return err
	}
	for i := range target.Tables[0].Columns {
		switch target.Tables[0].Columns[i].Name {
		case "id":
			target.Tables[0].Columns[i].Name = "customer_id"
		case "customer_name":
			target.Tables[0].Columns[i].Name = "full_name"
		}
	}
	target.Tables[0].PrimaryKey = []string{"customer_id"}
	for i := range target.Tables[0].Indexes {
		if target.Tables[0].Indexes[i].Name == "customers_name_idx" {
			target.Tables[0].Indexes[i].Columns = []string{"full_name", "customer_id"}
		}
	}
	target.Tables[0].Columns = append(target.Tables[0].Columns, schema.Column{Name: "notes", Type: "text", Nullable: true})
	target.Tables[0].Indexes = append(target.Tables[0].Indexes, schema.Index{Name: "customers_notes_idx", Columns: []string{"notes"}})
	target.Tables = append(target.Tables, schema.Table{Name: "events", Columns: []schema.Column{{Name: "id", Type: "bigint"}, {Name: "customer_id", Type: "bigint"}}, PrimaryKey: []string{"id"}, Indexes: []schema.Index{{Name: "events_customer_idx", Columns: []string{"customer_id"}}}})
	targetJSON, err := target.CanonicalJSON()
	if err != nil {
		return err
	}
	if err = write("desired.json", append(targetJSON, '\n')); err != nil {
		return err
	}
	renames := []byte("version: 1\ncolumns:\n- table: customers\n  from: id\n  to: customer_id\n- table: customers\n  from: customer_name\n  to: full_name\n")
	if err = write("renames.yaml", renames); err != nil {
		return err
	}
	sourcePath, targetPath, renamePath := filepath.Join(o.output, "current.json"), filepath.Join(o.output, "desired.json"), filepath.Join(o.output, "renames.yaml")
	artifact, err := cli(0, "reviewed-plan.json", "plan", sourcePath, targetPath, renamePath)
	if err != nil {
		return err
	}
	p, err := plan.Load(bytes.NewReader(artifact))
	if err != nil {
		return err
	}
	if len(p.Operations) != 6 {
		return fmt.Errorf("expected six documented operations, got %d", len(p.Operations))
	}
	r.Operations = len(p.Operations)
	review, err := cli(0, "review.txt", "plan", "--explain", sourcePath, targetPath, renamePath)
	if err != nil {
		return err
	}
	log("Review printed before disposable execution:\n%s", review)
	planPath := filepath.Join(o.output, "reviewed-plan.json")
	applied, err := cli(0, "apply-result.json", "apply", planPath)
	if err != nil {
		return err
	}
	var result postgres.ApplyResult
	if err = json.Unmarshal(applied, &result); err != nil {
		return err
	}
	if result.Status != "applied" || result.Operations != 6 {
		return fmt.Errorf("unexpected apply result")
	}
	r.PlanID = result.PlanID
	r.MigrationID = result.MigrationID
	matching, err := cli(0, "verify-match.json", "verify", "--plan", planPath)
	if err != nil {
		return err
	}
	var verification postgres.VerifyResult
	if err = json.Unmarshal(matching, &verification); err != nil || verification.Status != "match" || len(verification.Differences) != 0 {
		return fmt.Errorf("target verification did not match")
	}
	after, err := rows(true)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, after) {
		return fmt.Errorf("original customer values were not preserved")
	}
	r.RowsPreserved = len(after)
	afterJSON, _ := json.MarshalIndent(after, "", "  ")
	if err = write("after-rows.json", append(afterJSON, '\n')); err != nil {
		return err
	}
	var nullNotes int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM "+r.Schema+".customers WHERE notes IS NULL").Scan(&nullNotes); err != nil || nullNotes != 2 {
		return fmt.Errorf("nullable addition did not preserve NULL for existing rows")
	}
	if _, err = cli(5, "replay-error.txt", "apply", planPath); err != nil {
		return err
	}
	r.ReplayExit = 5
	if _, err = db.ExecContext(ctx, "ALTER TABLE "+r.Schema+".customers ADD COLUMN external_note text"); err != nil {
		return err
	}
	beforeVerify, err := cli(0, "before-drift-check.json", "inspect", r.Schema)
	if err != nil {
		return err
	}
	drift, err := cli(4, "verify-drift.json", "verify", "--plan", planPath)
	if err != nil {
		return err
	}
	r.DriftExit = 4
	if err = json.Unmarshal(drift, &verification); err != nil || verification.Status != "drift" || len(verification.Differences) != 1 || verification.Differences[0].Kind != "column_unexpected" {
		return fmt.Errorf("unexpected drift report")
	}
	afterVerify, err := cli(0, "after-drift-check.json", "inspect", r.Schema)
	if err != nil {
		return err
	}
	if !bytes.Equal(beforeVerify, afterVerify) {
		return fmt.Errorf("verification changed the observed schema")
	}
	var historyRows int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM forgeschema_meta.migrations WHERE migration_id=$1 AND plan_id=$2 AND operation_count=6", r.MigrationID, r.PlanID).Scan(&historyRows); err != nil || historyRows != 1 {
		return fmt.Errorf("history does not contain exactly the demonstrated migration")
	}
	// This is fixture cleanup in our own new demo schema, not a supported engine
	// DROP operation. Restore the target so --keep-schema leaves a matching state.
	if _, err = db.ExecContext(ctx, "ALTER TABLE "+r.Schema+".customers DROP COLUMN external_note"); err != nil {
		return err
	}
	if _, err = cli(0, "verify-final.json", "verify", targetPath); err != nil {
		return err
	}
	log("PASS: six operations, two original rows preserved, stale replay rejected, drift detected without repair, target restored.")
	return nil
}
