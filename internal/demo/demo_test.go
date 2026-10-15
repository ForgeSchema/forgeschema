package demo

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUsageAndExplicitDatabase(t *testing.T) {
	t.Setenv("FORGESCHEMA_DEMO_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "must-not-be-used")
	for _, c := range []struct {
		args []string
		code int
	}{{nil, 2}, {[]string{"-h"}, 0}, {[]string{"--unknown"}, 2}, {[]string{"extra"}, 2}} {
		var out, errs bytes.Buffer
		if got := Run(context.Background(), c.args, &out, &errs); got != c.code {
			t.Fatalf("%v: %d %s", c.args, got, errs.String())
		}
	}
}
func TestNeverOverwritesOutput(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "binary")
	if err := os.WriteFile(binary, []byte("placeholder"), 0700); err != nil {
		t.Fatal(err)
	}
	outdir := filepath.Join(dir, "already-there")
	if err := os.Mkdir(outdir, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outdir, "keep")
	if err := os.WriteFile(marker, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	err := execute(context.Background(), options{binary: binary, output: outdir}, "not-a-real-connection", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "NEW output") {
		t.Fatalf("did not reject existing directory: %v", err)
	}
	b, _ := os.ReadFile(marker)
	if string(b) != "unchanged" {
		t.Fatal("changed existing output")
	}
}
