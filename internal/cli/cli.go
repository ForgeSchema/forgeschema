package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
	"io"
	"os"
	"time"
)

const Version = "0.0.2-dev"
const usage = `ForgeSchema — PostgreSQL schema evolution prototype
Usage:
  forgeschema version
  forgeschema validate SCHEMA.yaml
  forgeschema inspect SCHEMA_NAME
  forgeschema help

Inspection uses DATABASE_URL and reads catalog metadata only, with a 30-second
timeout. PostgreSQL 16 and 17 are supported. Plan, apply and verify are pending.
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
