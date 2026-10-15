package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommands(t *testing.T) {
	for _, c := range []struct {
		args []string
		code int
	}{{nil, 2}, {[]string{"help"}, 0}, {[]string{"version"}, 0}, {[]string{"version", "extra"}, 2}, {[]string{"inspect"}, 2}, {[]string{"validate"}, 2}, {[]string{"validate", "missing.yaml"}, 1}, {[]string{"validate", "../../examples/customers.yaml"}, 0}} {
		var out, errs bytes.Buffer
		if got := Run(c.args, &out, &errs); got != c.code {
			t.Fatalf("%v: got %d want %d: %s", c.args, got, c.code, errs.String())
		}
		if c.code != 0 && out.Len() != 0 {
			t.Fatal("error leaked to stdout")
		}
	}
}
func TestInvalidFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(p, []byte("version: 999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if Run([]string{"validate", p}, &out, &errs) != 1 || !strings.Contains(errs.String(), "forgeschema:") {
		t.Fatal("missing diagnostic")
	}
}

func TestInspectionConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	var out, errs bytes.Buffer
	if got := Run([]string{"inspect", "public"}, &out, &errs); got != 2 || !strings.Contains(errs.String(), "DATABASE_URL") {
		t.Fatalf("missing configuration: %d %s", got, errs.String())
	}
	out.Reset()
	errs.Reset()
	t.Setenv("DATABASE_URL", "postgres://user:do-not-print-this@127.0.0.1:invalid/database")
	if got := Run([]string{"inspect", "public"}, &out, &errs); got != 1 || strings.Contains(errs.String(), "do-not-print-this") || out.Len() != 0 {
		t.Fatalf("unsafe connection diagnostic: %d %s", got, errs.String())
	}
	out.Reset()
	errs.Reset()
	if got := Run([]string{"inspect", "public; SELECT 1"}, &out, &errs); got != 1 || out.Len() != 0 {
		t.Fatalf("invalid scope accepted: %d %s", got, errs.String())
	}
}

func TestPlanCommands(t *testing.T) {
	cases := []struct {
		args []string
		code int
		has  string
	}{
		{[]string{"plan"}, 2, ""},
		{[]string{"plan", "../../examples/customers.yaml", "../../examples/customers.yaml"}, 0, `"operations": []`},
		{[]string{"plan", "../../examples/customers.yaml", "../../examples/customers-next.yaml", "../../examples/customer-renames.yaml"}, 0, `"rename_column"`},
		{[]string{"plan", "--explain", "../../examples/customers.yaml", "../../examples/customers-next.yaml", "../../examples/customer-renames.yaml"}, 0, `RENAME COLUMN`},
		{[]string{"plan", "../../examples/customers.yaml", "../../examples/customers-next.yaml"}, 3, `"status": "blocked"`},
		{[]string{"plan", "missing.yaml", "../../examples/customers.yaml"}, 1, ""},
	}
	for _, c := range cases {
		var out, errs bytes.Buffer
		got := Run(c.args, &out, &errs)
		if got != c.code {
			t.Fatalf("%v: %d %s", c.args, got, errs.String())
		}
		if c.has != "" && !strings.Contains(out.String(), c.has) {
			t.Fatalf("missing %s in %s", c.has, out.String())
		}
		if c.code == 1 || c.code == 2 {
			if out.Len() != 0 {
				t.Fatal("invalid input produced artifact")
			}
		}
	}
}
