package cli

import (
	"fmt"
	"github.com/ForgeSchema/forgeschema/schema"
	"io"
	"os"
)

const Version = "0.0.1-dev"
const usage = `ForgeSchema — PostgreSQL schema evolution prototype
Usage:
  forgeschema version
  forgeschema validate SCHEMA.yaml
  forgeschema help

This checkpoint validates schema files only. Inspect, plan, apply and verify
are planned; this binary does not connect to or modify a database.
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
