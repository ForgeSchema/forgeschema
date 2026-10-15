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
