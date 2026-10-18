package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
)

const Version = "0.2.0-alpha.1"
const planUsage = "usage: forgeschema plan [--explain] [--online] CURRENT.yaml DESIRED.yaml [RENAMES.yaml]"

const usage = `ForgeSchema — PostgreSQL schema evolution prototype
Usage:
  forgeschema version
  forgeschema validate SCHEMA.yaml
  forgeschema inspect SCHEMA_NAME
  forgeschema plan [--explain] [--online] CURRENT.yaml DESIRED.yaml [RENAMES.yaml]
  forgeschema apply [--lock-timeout 5s] [--statement-timeout 30s] [--timeout 60s] PLAN.json
  forgeschema verify EXPECTED.yaml
  forgeschema verify --plan PLAN.json
  forgeschema help

Inspection uses DATABASE_URL and reads catalog metadata only, with a 30-second
timeout. PostgreSQL 16 and 17 are supported. Planning is offline and does not
execute SQL; --online builds indexes on existing tables concurrently. A
blocked plan exits 3. Apply executes an exact reviewed artifact;
pause external DDL first. It exits 5 when the database no longer matches the
plan and 6 when the commit outcome is unknown. Verify is read-only and exits 4
on structural drift.
`

func Run(args []string, out, errs io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(errs, usage)
		return 2
	}
	switch args[0] {
	case "help", "--help", "-h":
		if len(args) != 1 {
			return fail(errs, "help takes no arguments", 2)
		}
		fmt.Fprint(out, usage)
		return 0
	case "version":
		if len(args) != 1 {
			return fail(errs, "version takes no arguments", 2)
		}
		fmt.Fprintln(out, Version)
		return 0
	case "verify":
		var expected schema.Document
		if len(args) == 2 && args[1] != "--plan" {
			var err error
			expected, err = loadSchema(args[1])
			if err != nil {
				return fail(errs, err.Error(), 1)
			}
		} else if len(args) == 3 && args[1] == "--plan" {
			f, err := os.Open(args[2])
			if err != nil {
				return fail(errs, err.Error(), 1)
			}
			p, err := plan.Load(f)
			f.Close()
			if err != nil {
				return fail(errs, err.Error(), 1)
			}
			if p.Status != "ready" {
				return fail(errs, "blocked plans cannot be verified as applied targets", 3)
			}
			expected = p.Target
		} else {
			return fail(errs, "usage: forgeschema verify EXPECTED.yaml | verify --plan PLAN.json (set DATABASE_URL)", 2)
		}
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			return fail(errs, "DATABASE_URL is required for verification", 2)
		}
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return fail(errs, "invalid database connection configuration", 1)
		}
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := postgres.Verify(ctx, db, expected)
		if err != nil {
			var unsupported *postgres.UnsupportedError
			if errors.As(err, &unsupported) {
				return fail(errs, unsupported.Error(), 1)
			}
			return fail(errs, "database verification failed"+detail(err)+"; check connection, permissions, server version and timeout", 1)
		}
		b, err := json.Marshal(result)
		if err != nil {
			return fail(errs, "could not encode verification result", 1)
		}
		fmt.Fprintln(out, string(b))
		if result.Status == "drift" {
			return 4
		}
		return 0
	case "apply":
		flags := flag.NewFlagSet("apply", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		var opts postgres.ApplyOptions
		flags.DurationVar(&opts.LockTimeout, "lock-timeout", 5*time.Second, "")
		flags.DurationVar(&opts.StatementTimeout, "statement-timeout", 30*time.Second, "")
		flags.DurationVar(&opts.Timeout, "timeout", 60*time.Second, "")
		applyUsage := "usage: forgeschema apply [--lock-timeout 5s] [--statement-timeout 30s] [--timeout 60s] PLAN.json (set DATABASE_URL; pause external DDL)"
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 1 {
			return fail(errs, applyUsage, 2)
		}
		if opts.LockTimeout < time.Millisecond || opts.StatementTimeout < time.Millisecond || opts.Timeout < time.Millisecond {
			return fail(errs, "timeouts must be at least 1ms, for example 30s or 5m", 2)
		}
		args = []string{"apply", flags.Arg(0)}
		f, err := os.Open(args[1])
		if err != nil {
			return fail(errs, err.Error(), 1)
		}
		p, err := plan.Load(f)
		f.Close()
		if err != nil {
			return fail(errs, err.Error(), 1)
		}
		if p.Status != "ready" {
			return fail(errs, "blocked plans cannot be applied", 3)
		}
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			return fail(errs, "DATABASE_URL is required for apply", 2)
		}
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return fail(errs, "invalid database connection configuration", 1)
		}
		defer db.Close()
		result, err := postgres.Apply(context.Background(), db, p, opts)
		if err != nil {
			var commit *postgres.CommitError
			if errors.As(err, &commit) {
				return fail(errs, commit.Error()+"; plan_id="+commit.PlanID+" migration_id="+commit.MigrationID, 6)
			}
			var drift *postgres.DriftError
			if errors.As(err, &drift) {
				if drift.Phase == "target" {
					return fail(errs, drift.Error()+"; the result did not match the reviewed target, so nothing was committed. Check for concurrent DDL, then inspect again", 5)
				}
				return fail(errs, drift.Error()+"; the database no longer matches this plan, so inspect it again and build a new plan", 5)
			}
			var stage *postgres.ApplyError
			if errors.As(err, &stage) {
				// The connection could not be opened: keep that message
				// generic, since driver errors there can echo connection details.
				if stage.Stage == "begin" {
					return fail(errs, "could not start the migration transaction; check DATABASE_URL, network and permissions", 1)
				}
				if stage.Resumable {
					return fail(errs, "migration stopped during "+stage.Stage+detail(err)+"; the first phase of this online plan is committed and no history was recorded, so fix the cause and apply the same plan again to finish it", 1)
				}
				return fail(errs, "migration failed during "+stage.Stage+detail(err)+"; transaction was not committed", 1)
			}
			return fail(errs, "could not apply reviewed migration plan", 1)
		}
		b, err := json.Marshal(result)
		if err != nil {
			return fail(errs, "could not encode apply result", 1)
		}
		fmt.Fprintln(out, string(b))
		return 0
	case "plan":
		inputs := args[1:]
		explain, online := false, false
		for len(inputs) > 0 && strings.HasPrefix(inputs[0], "--") {
			switch {
			case inputs[0] == "--explain" && !explain:
				explain = true
			case inputs[0] == "--online" && !online:
				online = true
			default:
				return fail(errs, planUsage, 2)
			}
			inputs = inputs[1:]
		}
		if len(inputs) < 2 || len(inputs) > 3 {
			return fail(errs, planUsage, 2)
		}
		src, err := loadSchema(inputs[0])
		if err != nil {
			return fail(errs, err.Error(), 1)
		}
		dst, err := loadSchema(inputs[1])
		if err != nil {
			return fail(errs, err.Error(), 1)
		}
		renames := []plan.Rename{}
		if len(inputs) == 3 {
			f, err := os.Open(inputs[2])
			if err != nil {
				return fail(errs, err.Error(), 1)
			}
			renames, err = plan.LoadRenames(f)
			f.Close()
			if err != nil {
				return fail(errs, err.Error(), 1)
			}
		}
		p, err := plan.BuildWith(src, dst, renames, plan.Options{Online: online})
		if err != nil {
			return fail(errs, err.Error(), 1)
		}
		if explain {
			summary, err := p.Summary()
			if err != nil {
				return fail(errs, err.Error(), 1)
			}
			fmt.Fprint(out, summary)
		} else {
			b, err := p.JSON()
			if err != nil {
				return fail(errs, err.Error(), 1)
			}
			fmt.Fprintln(out, string(b))
		}
		if p.Status == "blocked" {
			return 3
		}
		return 0
	case "inspect":
		if len(args) != 2 {
			return fail(errs, "usage: forgeschema inspect SCHEMA_NAME (set DATABASE_URL)", 2)
		}
		if _, err := schema.Normalize(schema.Document{Version: 1, Schema: args[1], Tables: []schema.Table{}}); err != nil {
			return fail(errs, err.Error(), 1)
		}
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			return fail(errs, "DATABASE_URL is required for inspection", 2)
		}
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return fail(errs, "invalid database connection configuration", 1)
		}
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d, err := postgres.Inspect(ctx, db, args[1])
		if err != nil {
			var unsupported *postgres.UnsupportedError
			if errors.As(err, &unsupported) {
				return fail(errs, unsupported.Error(), 1)
			}
			return fail(errs, "database inspection failed"+detail(err)+"; check connection, permissions, server version and timeout", 1)
		}
		b, err := d.CanonicalJSON()
		if err != nil {
			return fail(errs, "could not encode inspected schema", 1)
		}
		fmt.Fprintln(out, string(b))
		return 0
	case "validate":
		if len(args) != 2 {
			return fail(errs, "usage: forgeschema validate SCHEMA.yaml", 2)
		}
		f, err := os.Open(args[1])
		if err != nil {
			return fail(errs, err.Error(), 1)
		}
		defer f.Close()
		d, err := schema.Load(f)
		if err != nil {
			return fail(errs, err.Error(), 1)
		}
		b, err := d.CanonicalJSON()
		if err != nil {
			return fail(errs, err.Error(), 1)
		}
		fmt.Fprintln(out, string(b))
		return 0
	default:
		return fail(errs, fmt.Sprintf("unknown command %q; use help", args[0]), 2)
	}
}

// detail returns the server's error message and SQLSTATE, or a plain
// timeout/cancellation note. It leaves out the error's Detail field, which can
// quote row values such as a duplicated key, and stays silent for connection,
// authentication and missing-database errors (SQLSTATE classes 08, 28, 3D),
// whose messages can name the user or database from DATABASE_URL.
func detail(err error) string {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Code.Class() {
		case "08", "28", "3D":
			return ""
		}
		return ": " + pqErr.Message + " [SQLSTATE " + string(pqErr.Code) + "]"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ": timed out"
	}
	if errors.Is(err, context.Canceled) {
		return ": canceled"
	}
	return ""
}
func fail(w io.Writer, message string, code int) int {
	fmt.Fprintln(w, "forgeschema:", message)
	return code
}

func loadSchema(path string) (schema.Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return schema.Document{}, err
	}
	defer f.Close()
	d, err := schema.Load(f)
	if err != nil {
		return d, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}
