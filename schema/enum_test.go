package schema

import (
	"strings"
	"testing"
)

func load(t *testing.T, y string) (Document, error) {
	t.Helper()
	return Load(strings.NewReader(y))
}

const enumDoc = `version: 1
schema: shop
enums:
- name: status
  values: [active, "on hold", "it's closed"]
tables:
- name: orders
  columns:
  - name: id
    type: bigint
  - name: state
    type: status
    default: "'active'"
  - name: tags
    type: TEXT []
    default: "'{}'"
  - name: scores
    type: int[]
    nullable: true
  - name: history
    type: status[]
    nullable: true
  - name: codes
    type: varchar(12)[]
    nullable: true
  primary_key: [id]
  indexes:
  - name: orders_state
    columns: [state]
`

func TestEnumsAndArraysNormalize(t *testing.T) {
	d, err := load(t, enumDoc)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	defaults := map[string]string{}
	for _, c := range d.Tables[0].Columns {
		types[c.Name] = c.Type
		defaults[c.Name] = c.Default
	}
	want := map[string]string{"state": "status", "tags": "text[]", "scores": "integer[]", "history": "status[]", "codes": "character varying(12)[]"}
	for k, v := range want {
		if types[k] != v {
			t.Errorf("%s: type %q, want %q", k, types[k], v)
		}
	}
	if defaults["state"] != "'active'" || defaults["tags"] != "'{}'" {
		t.Errorf("defaults %v", defaults)
	}
	if len(d.Enums) != 1 || strings.Join(d.Enums[0].Values, "|") != "active|on hold|it's closed" {
		t.Errorf("enums %+v", d.Enums)
	}
	again, err := Normalize(d)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := d.CanonicalJSON()
	b, _ := again.CanonicalJSON()
	if string(a) != string(b) {
		t.Fatal("not idempotent")
	}
}

func TestDocumentsWithoutEnumsKeepTheirJSON(t *testing.T) {
	d := loadShop(t)
	b, _ := d.CanonicalJSON()
	if strings.Contains(string(b), "enums") {
		t.Fatal("empty enums appear in canonical JSON")
	}
}

func TestEnumAndArrayRefusals(t *testing.T) {
	cases := map[string]string{
		"unknown type":      strings.Replace(enumDoc, "type: status\n", "type: mood\n", 1),
		"nested array":      strings.Replace(enumDoc, "type: int[]", "type: int[][]", 1),
		"bad enum default":  strings.Replace(enumDoc, `default: "'active'"`, `default: "'gone'"`, 1),
		"bad array default": strings.Replace(enumDoc, `default: "'{}'"`, `default: "'{a}'"`, 1),
		"builtin name":      strings.Replace(enumDoc, "- name: status\n", "- name: text\n", 1),
		"empty enum":        strings.Replace(enumDoc, `values: [active, "on hold", "it's closed"]`, "values: []", 1),
		"repeated value":    strings.Replace(enumDoc, `values: [active, "on hold", "it's closed"]`, "values: [a, a]", 1),
		"long value":        strings.Replace(enumDoc, `"on hold"`, `"`+strings.Repeat("x", 64)+`"`, 1),
		"table clash":       strings.Replace(enumDoc, "- name: status\n", "- name: orders\n", 1),
		"array clash":       strings.Replace(enumDoc, "- name: status\n", "- name: _orders\n", 1),
		"enum check": strings.Replace(enumDoc, "  primary_key: [id]\n", `  primary_key: [id]
  checks:
  - name: orders_state_check
    expression: state = 'active'
`, 1),
	}
	for name, y := range cases {
		if _, err := load(t, y); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEnumAndArrayCatalogForms(t *testing.T) {
	enums := map[string]Enum{"status": {Name: "status", Values: []string{"active", "it's closed"}}}
	types := map[string]string{
		"shop.status":             "status",
		`"shop".status`:           "status",
		"shop.status[]":           "status[]",
		"integer[]":               "integer[]",
		"character varying(12)[]": "character varying(12)[]",
		"text":                    "text",
	}
	for in, want := range types {
		if got, ok := TypeFromCatalog("shop", in, enums); !ok || got != want {
			t.Errorf("type %q: got %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"other.status", "shop.mood", "status_x", "public.status[]"} {
		if _, ok := TypeFromCatalog("shop", bad, enums); ok {
			t.Errorf("type %q accepted", bad)
		}
	}
	defaults := []struct{ typ, expr, want string }{
		{"status", "'active'::shop.status", "'active'"},
		{"status", "'it''s closed'::shop.status", "'it''s closed'"},
		{"text[]", "'{}'::text[]", "'{}'"},
		{"character varying(12)[]", "'{}'::character varying[]", "'{}'"},
		{"status[]", "'{}'::shop.status[]", "'{}'"},
	}
	for _, c := range defaults {
		if got, ok := ColumnDefaultFromCatalog("shop", c.typ, c.expr, enums); !ok || got != c.want {
			t.Errorf("default %q: got %q %v", c.expr, got, ok)
		}
	}
	for _, c := range []struct{ typ, expr string }{
		{"status", "'gone'::shop.status"},
		{"status", "'active'::other.status"},
		{"text[]", "'{a}'::text[]"},
		{"text[]", "ARRAY['a'::text]"},
	} {
		if _, ok := ColumnDefaultFromCatalog("shop", c.typ, c.expr, enums); ok {
			t.Errorf("default %q accepted", c.expr)
		}
	}
}
