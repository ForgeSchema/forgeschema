package plan

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func samplePlan(t *testing.T) Plan {
	t.Helper()
	p, err := Build(doc(t, base), doc(t, strings.Replace(base, "customer_name", "full_name", 1)), []Rename{{"customers", "customer_name", "full_name"}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestStrictPlanLoad(t *testing.T) {
	p := samplePlan(t)
	b, err := p.JSON()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := p.ID()
	c, _ := loaded.ID()
	if a != c {
		t.Fatal("whitespace changed identity")
	}
	compact, _ := json.Marshal(p)
	again, err := Load(bytes.NewReader(compact))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := again.ID()
	if id != a {
		t.Fatal("indent changed identity")
	}
	s := string(b)
	cases := map[string]string{
		"unknown":           strings.Replace(s, `"format_version": 1`, `"unknown": 1, "format_version": 1`, 1),
		"duplicate":         strings.Replace(s, `"format_version": 1`, `"format_version": 1, "format_version": 1`, 1),
		"duplicate-escaped": strings.Replace(s, `"format_version": 1`, `"format_version": 1, "\u0066ormat_version": 1`, 1),
		"nested-duplicate":  strings.Replace(s, `"nullable": true`, `"nullable": true, "nullable": true`, 1),
		"null":              strings.Replace(s, `"renames": [`, `"renames": null, "extra": [`, 1),
		"case":              strings.Replace(s, `"format_version"`, `"Format_version"`, 1),
		"quoted-number":     strings.Replace(s, `"format_version": 1`, `"format_version": "1"`, 1),
		"decimal":           strings.Replace(s, `"format_version": 1`, `"format_version": 1.0`, 1),
		"missing":           strings.Replace(s, `"format_version": 1,`, "", 1),
		"missing-bool":      strings.Replace(s, `"nullable": true`, `"name_removed": true`, 1),
		"trailing":          s + ` {}`,
		"invalid-root":      "[]",
		"tampered-sql":      strings.Replace(s, "RENAME COLUMN", "DROP COLUMN", 1),
		"bad-utf8":          string([]byte{0xff}),
		"oversized":         strings.Repeat(" ", MaxPlanBytes+1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Load(strings.NewReader(input))
			if err == nil {
				t.Fatal("accepted malformed artifact")
			}
			if got.FormatVersion != 0 {
				t.Fatal("returned partial artifact")
			}
		})
	}
}
func TestBlockedPlanLoadsButHasNoOperations(t *testing.T) {
	p, err := Build(doc(t, base), doc(t, strings.Replace(base, "type: bigint", "type: integer", 1)), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := p.JSON()
	loaded, err := Load(bytes.NewReader(b))
	if err != nil || loaded.Status != "blocked" || len(loaded.Operations) != 0 {
		t.Fatalf("%+v %v", loaded, err)
	}
}
func FuzzPlanLoader(f *testing.F) {
	f.Add(`{}`)
	f.Add(`{"format_version":1}`)
	f.Fuzz(func(t *testing.T, input string) {
		p, err := Load(strings.NewReader(input))
		if err == nil {
			if err = p.Validate(); err != nil {
				t.Fatal(err)
			}
		}
	})
}
