package schema

import (
	"os"
	"strings"
	"testing"
)

const valid = `version: 1
schema: public
tables:
- name: customers
  columns:
  - name: id
    type: int8
  - name: email
    type: text
    nullable: true
  primary_key: [id]
  indexes:
  - name: customers_email_idx
    columns: [email]
`

func TestLoadAndCanonical(t *testing.T) {
	d, err := Load(strings.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	if d.Tables[0].Columns[1].Type != "bigint" {
		t.Fatal("alias not normalized")
	}
	if d.Tables[0].Columns[1].Nullable {
		t.Fatal("omitted nullable must be false")
	}
	a, err := d.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	d.Tables[0].Columns[0], d.Tables[0].Columns[1] = d.Tables[0].Columns[1], d.Tables[0].Columns[0]
	b, err := d.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("order changed fingerprint")
	}
	if d.Tables[0].Columns[0].Name != "id" {
		t.Fatal("normalization mutated caller")
	}
	changed := d
	changed.Tables[0].Columns[0].Type = "integer"
	c, _ := changed.Fingerprint()
	if c == a {
		t.Fatal("type change did not change fingerprint")
	}
}
func TestRejectInvalidYAML(t *testing.T) {
	cases := map[string]string{
		"unknown":               valid + "extra: true\n",
		"duplicate":             valid + "schema: other\n",
		"multi":                 valid + "---\nversion: 1\n",
		"null":                  strings.Replace(valid, "type: int8", "type: null", 1),
		"number-as-name":        strings.Replace(valid, "name: id", "name: 42", 1),
		"quoted-version":        strings.Replace(valid, "version: 1", `version: "1"`, 1),
		"quoted-bool":           strings.Replace(valid, "nullable: true", `nullable: "true"`, 1),
		"anchor":                strings.Replace(valid, "schema: public", "schema: &s public", 1),
		"alias":                 strings.Replace(valid, "schema: public", "schema: *s", 1),
		"tag":                   strings.Replace(valid, "schema: public", "schema: !custom public", 1),
		"nested-unknown":        strings.Replace(valid, "type: int8", "type: int8\n    default: 1", 1),
		"missing-version":       strings.Replace(valid, "version: 1\n", "", 1),
		"missing-tables":        "version: 1\nschema: public\n",
		"unsupported-type":      strings.Replace(valid, "type: int8", "type: serial", 1),
		"invalid-name":          strings.Replace(valid, "name: customers", "name: Customers", 1),
		"reserved-schema":       strings.Replace(valid, "schema: public", "schema: pg_catalog", 1),
		"nullable-pk":           strings.Replace(valid, "type: int8", "type: int8\n    nullable: true", 1),
		"missing-pk-column":     strings.Replace(valid, "primary_key: [id]", "primary_key: [missing]", 1),
		"duplicate-pk-column":   strings.Replace(valid, "primary_key: [id]", "primary_key: [id, id]", 1),
		"empty-index":           strings.Replace(valid, "columns: [email]", "columns: []", 1),
		"missing-index-column":  strings.Replace(valid, "columns: [email]", "columns: [missing]", 1),
		"index-table-collision": strings.Replace(valid, "name: customers_email_idx", "name: customers", 1),
		"index-pk-collision":    strings.Replace(valid, "name: customers_email_idx", "name: customers_pkey", 1),
		"empty":                 "",
		"oversized":             strings.Repeat(" ", MaxBytes+1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(strings.NewReader(input)); err == nil {
				t.Fatal("accepted invalid schema")
			}
		})
	}
}
func TestDirectValidation(t *testing.T) {
	d, err := Load(strings.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	cases := []Document{
		{Version: 1, Schema: "public", Tables: []Table{d.Tables[0], d.Tables[0]}},
		{Version: 1, Schema: "public", Tables: []Table{{Name: "t", Columns: []Column{{Name: "x", Type: "text"}, {Name: "x", Type: "text"}}}}},
		{Version: 1, Schema: "public", Tables: []Table{{Name: "t"}}},
		{Version: 1, Schema: "public", Tables: []Table{{Name: strings.Repeat("a", 64), Columns: []Column{{Name: "id", Type: "bigint"}}}}},
	}
	for _, c := range cases {
		if _, err := Normalize(c); err == nil {
			t.Fatal("accepted invalid Go document")
		}
	}
}
func TestExamples(t *testing.T) {
	for _, name := range []string{"customers", "empty"} {
		f, err := os.Open("../examples/" + name + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		_, err = Load(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
func TestKeyOrderSignificant(t *testing.T) {
	d, err := Load(strings.NewReader(strings.Replace(valid, "nullable: true", "nullable: false", 1)))
	if err != nil {
		t.Fatal(err)
	}
	d.Tables[0].PrimaryKey = []string{"id", "email"}
	a, _ := d.Fingerprint()
	d.Tables[0].PrimaryKey = []string{"email", "id"}
	b, _ := d.Fingerprint()
	if a == b {
		t.Fatal("key order must be significant")
	}
}
func FuzzLoad(f *testing.F) {
	f.Add(valid)
	f.Add("version: 1\nschema: public\ntables: []\n")
	f.Fuzz(func(t *testing.T, input string) {
		d, err := Load(strings.NewReader(input))
		if err == nil {
			a, err := d.CanonicalJSON()
			if err != nil {
				t.Fatal(err)
			}
			b, err := d.CanonicalJSON()
			if err != nil || string(a) != string(b) {
				t.Fatal("non deterministic canonical form")
			}
		}
	})
}

func TestLimitsAndKeyAnchors(t *testing.T) {
	if _, err := Load(strings.NewReader(strings.Replace(valid, "schema: public", "&key schema: public", 1))); err == nil {
		t.Fatal("accepted anchored key")
	}
	d, err := Load(strings.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	d.Tables[0].Indexes[0].Columns = make([]string, 33)
	if _, err := Normalize(d); err == nil {
		t.Fatal("accepted oversized index")
	}
	d.Tables[0].Indexes = nil
	d.Tables[0].Columns = make([]Column, 1601)
	if _, err := Normalize(d); err == nil {
		t.Fatal("accepted oversized table")
	}
}
