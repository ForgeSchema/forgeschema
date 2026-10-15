package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
)

const Version = "0.0.5-dev"
const usage = `ForgeSchema — PostgreSQL schema evolution prototype
Usage:
  forgeschema version
  forgeschema validate SCHEMA.yaml
  forgeschema inspect SCHEMA_NAME
  forgeschema plan [--explain] CURRENT.yaml DESIRED.yaml [RENAMES.yaml]
  forgeschema apply PLAN.json
  forgeschema verify EXPECTED.yaml
  forgeschema verify --plan PLAN.json
  forgeschema help

Inspection uses DATABASE_URL and reads catalog metadata only, with a 30-second
timeout. PostgreSQL 16 and 17 are supported. Planning is offline and does not
execute SQL. A blocked plan exits 3. Apply executes an exact reviewed artifact;
pause external DDL first. Verify is read-only and exits 4 on structural drift.
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
			return fail(errs, "database verification failed; check connection, permissions, server version and timeout", 1)
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
		if len(args) != 2 {
			return fail(errs, "usage: forgeschema apply PLAN.json (set DATABASE_URL; pause external DDL)", 2)
		}
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
		result, err := postgres.Apply(context.Background(), db, p, postgres.ApplyOptions{})
		if err != nil {
			var commit *postgres.CommitError
			if errors.As(err, &commit) {
				return fail(errs, commit.Error()+"; plan_id="+commit.PlanID+" migration_id="+commit.MigrationID, 1)
			}
			var drift *postgres.DriftError
			if errors.As(err, &drift) {
				return fail(errs, drift.Error(), 1)
			}
			var stage *postgres.ApplyError
			if errors.As(err, &stage) {
				return fail(errs, "migration failed during "+stage.Stage+"; transaction was not committed", 1)
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
		explain := false
		if len(inputs) > 0 && inputs[0] == "--explain" {
			explain = true
			inputs = inputs[1:]
		}
		if len(inputs) < 2 || len(inputs) > 3 {
			return fail(errs, "usage: forgeschema plan [--explain] CURRENT.yaml DESIRED.yaml [RENAMES.yaml]", 2)
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
		p, err := plan.Build(src, dst, renames)
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
			return fail(errs, "database inspection failed; check connection, permissions, server version and timeout", 1)
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
