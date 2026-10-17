package schema

import (
	"os"
	"strings"
	"testing"
)

func loadShop(t *testing.T) Document {
	t.Helper()
	f, err := os.Open("../examples/shop.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestShopExampleNormalizes(t *testing.T) {
	d := loadShop(t)
	if d.Tables[0].Name != "customers" || d.Tables[0].Columns[2].Type != "character varying(320)" {
		t.Fatalf("type not normalized: %+v", d.Tables[0].Columns)
	}
	again, err := Normalize(d)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := d.CanonicalJSON()
	b, _ := again.CanonicalJSON()
	if string(a) != string(b) {
		t.Fatal("normalization is not idempotent")
	}
}

func TestNormalizeType(t *testing.T) {
	cases := map[string]string{
		"varchar(20)": "character varying(20)", "VARCHAR ( 20 )": "character varying(20)", "character varying": "character varying",
		"char": "character(1)", "char(3)": "character(3)", "character(3)": "character(3)",
		"numeric": "numeric", "decimal(10, 2)": "numeric(10,2)", "numeric(5)": "numeric(5,0)", "numeric(10,2)": "numeric(10,2)",
	}
	for in, want := range cases {
		if got, ok := NormalizeType(in); !ok || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"varchar(0)", "varchar(010)", "varchar(10485761)", "numeric(2,3)", "numeric(0)", "numeric(1001)", "numeric(5,-1)", "varchar(x)", "timestamp(3)", "integer[]"} {
		if got, ok := NormalizeType(bad); ok {
			t.Errorf("%q accepted as %q", bad, got)
		}
	}
}

func TestNormalizeDefault(t *testing.T) {
	good := []struct{ typ, in, want string }{
		{"integer", "0", "0"}, {"integer", "-2147483648", "-2147483648"}, {"bigint", "9223372036854775807", "9223372036854775807"},
		{"numeric(12,2)", "1.50", "1.50"}, {"numeric(5,2)", "-999.99", "-999.99"}, {"numeric(3,3)", "0.123", "0.123"}, {"double precision", "-0.5", "-0.5"}, {"boolean", "TRUE", "true"},
		{"text", "'it''s'", "'it''s'"}, {"character varying(3)", "'abc'", "'abc'"}, {"text", "''", "''"},
		{"timestamp with time zone", "NOW()", "now()"}, {"timestamp with time zone", "CURRENT_TIMESTAMP", "current_timestamp"},
		{"timestamp without time zone", "localtimestamp", "localtimestamp"}, {"date", "current_date", "current_date"},
		{"uuid", "gen_random_uuid()", "gen_random_uuid()"}, {"jsonb", "'{}'", "'{}'"},
	}
	for _, c := range good {
		if got, err := NormalizeDefault(c.typ, c.in); err != nil || got != c.want {
			t.Errorf("%s %q: got %q %v", c.typ, c.in, got, err)
		}
	}
	bad := []struct{ typ, in string }{
		{"integer", "1.5"}, {"integer", "2147483648"}, {"smallint", "40000"}, {"integer", "-0"}, {"numeric", "-0.0"}, {"integer", "01"},
		{"numeric", "1e3"}, {"boolean", "yes"}, {"text", "active"}, {"text", "'unterminated"}, {"text", "'a'||'b'"},
		{"character varying(2)", "'abc'"}, {"jsonb", `'{"a": 1}'`}, {"uuid", "'00000000-0000-0000-0000-000000000000'"},
		{"date", "now()"}, {"timestamp with time zone", "localtimestamp"}, {"bytea", "'x'"}, {"text", "lower('X')"},
		{"text", `'\''; COMMIT; DROP TABLE x; --'`}, {"text", `'a\b'`},
		{"numeric(3,0)", "12345"}, {"numeric(5,2)", "1.239"}, {"numeric(5,2)", "1000"}, {"real", "1" + strings.Repeat("0", 42)},
	}
	for _, c := range bad {
		if got, err := NormalizeDefault(c.typ, c.in); err == nil {
			t.Errorf("%s %q accepted as %q", c.typ, c.in, got)
		}
	}
}

// The catalog renderings below were observed from PostgreSQL 16 pg_get_expr.
func TestDefaultFromCatalog(t *testing.T) {
	cases := []struct{ typ, expr, want string }{
		{"integer", "0", "0"}, {"integer", "'-1'::integer", "-1"}, {"bigint", "'3000000000'::bigint", "3000000000"},
		{"numeric(10,2)", "1.5", "1.5"}, {"numeric", "'-2.25'::numeric", "-2.25"}, {"double precision", "'-0.5'::numeric", "-0.5"},
		{"real", "'-1.5'::numeric", "-1.5"}, {"boolean", "true", "true"}, {"text", "'it''s'::text", "'it''s'"},
		{"character varying(20)", "'v'::character varying", "'v'"}, {"character(3)", "'abc'::bpchar", "'abc'"},
		{"timestamp with time zone", "now()", "now()"}, {"timestamp with time zone", "CURRENT_TIMESTAMP", "current_timestamp"},
		{"timestamp without time zone", "LOCALTIMESTAMP", "localtimestamp"}, {"date", "CURRENT_DATE", "current_date"},
		{"uuid", "gen_random_uuid()", "gen_random_uuid()"}, {"jsonb", "'{}'::jsonb", "'{}'"}, {"smallint", "'-1'::integer", "-1"},
		{"real", "'3000000000'::bigint", "3000000000"}, {"numeric(5,2)", "'-1'::integer", "-1"},
	}
	for _, c := range cases {
		if got, ok := DefaultFromCatalog(c.typ, c.expr); !ok || got != c.want {
			t.Errorf("%s %q: got %q %v, want %q", c.typ, c.expr, got, ok, c.want)
		}
	}
	for _, c := range []struct{ typ, expr string }{
		{"integer", "nextval('t_id_seq'::regclass)"}, {"timestamp with time zone", "clock_timestamp()"},
		{"text", "lower('X'::text)"}, {"jsonb", `'{"a": 1}'::jsonb`}, {"timestamp with time zone", "'2024-01-01 00:00:00+00'::timestamp with time zone"},
		// Casts that change the stored value must not be stripped.
		{"text", "'ab '::bpchar"}, {"double precision", "'0.1'::real"}, {"numeric(10,4)", "'1.1'::real"}, {"real", "(0.1)::real"},
		{"text", "'a\\b'::text"}, {"integer", "5::bigint"}, {"text", "'x'::character varying"},
	} {
		if got, ok := DefaultFromCatalog(c.typ, c.expr); ok {
			t.Errorf("%s %q accepted as %q", c.typ, c.expr, got)
		}
	}
}

const extended = `version: 1
schema: public
tables:
- name: parents
  columns:
  - name: id
    type: integer
    identity: by_default
  - name: code
    type: text
  primary_key: [id]
  unique:
  - name: parents_code_key
    columns: [code]
- name: children
  columns:
  - name: id
    type: bigint
  - name: parent_id
    type: bigint
    nullable: true
  - name: parent_code
    type: varchar(10)
    nullable: true
  primary_key: [id]
  foreign_keys:
  - name: children_parent_fkey
    columns: [parent_id]
    references:
      table: parents
      columns: [id]
    on_delete: set_null
  - name: children_code_fkey
    columns: [parent_code]
    references:
      table: parents
      columns: [code]
    on_update: no_action
`

func TestExtendedValidation(t *testing.T) {
	d, err := Load(strings.NewReader(extended))
	if err != nil {
		t.Fatal(err)
	}
	if d.Tables[0].ForeignKeys[0].Name != "children_code_fkey" || d.Tables[0].ForeignKeys[0].OnUpdate != "" {
		t.Fatalf("foreign keys not canonical: %+v", d.Tables[0].ForeignKeys)
	}
	cases := map[string]string{
		"identity-nullable":    strings.Replace(extended, "identity: by_default", "identity: by_default\n    nullable: true", 1),
		"identity-type":        strings.Replace(extended, "type: integer\n    identity", "type: text\n    identity", 1),
		"identity-default":     strings.Replace(extended, "identity: by_default", "identity: by_default\n    default: \"1\"", 1),
		"identity-value":       strings.Replace(extended, "identity: by_default", "identity: sometimes", 1),
		"identity-seq-clash":   extended + "- name: parents_id_seq\n  columns:\n  - name: x\n    type: text\n",
		"unique-name-clash":    strings.Replace(extended, "name: parents_code_key", "name: children", 1),
		"unique-unknown":       strings.Replace(extended, "columns: [code]\n- name", "columns: [missing]\n- name", 1),
		"fk-unknown-table":     strings.Replace(extended, "table: parents\n      columns: [id]", "table: nowhere\n      columns: [id]", 1),
		"fk-unkeyed":           strings.Replace(extended, "unique:\n  - name: parents_code_key\n    columns: [code]\n", "", 1),
		"fk-type":              strings.Replace(extended, "type: varchar(10)", "type: integer", 1),
		"fk-count":             strings.Replace(extended, "columns: [parent_id]", "columns: [parent_id, parent_code]", 1),
		"fk-action":            strings.Replace(extended, "on_delete: set_null", "on_delete: explode", 1),
		"fk-set-null-required": strings.Replace(extended, "type: bigint\n    nullable: true", "type: bigint", 1),
		"fk-name-clash":        strings.Replace(extended, "name: children_code_fkey", "name: children_parent_fkey", 1),
		"fk-pk-name":           strings.Replace(extended, "name: children_code_fkey", "name: children_pkey", 1),
		"default-type":         strings.Replace(extended, "type: text\n  primary_key", "type: text\n    default: \"1\"\n  primary_key", 1),
	}
	for name, input := range cases {
		if _, err := Load(strings.NewReader(input)); err == nil {
			t.Errorf("%s: accepted invalid schema", name)
		}
	}
}

func TestExtendedCompare(t *testing.T) {
	base := `version: 1
schema: public
tables:
- name: t
  columns:
  - name: id
    type: integer
    identity: always
  - name: code
    type: text
    default: "'x'"
  - name: ref
    type: integer
    nullable: true
  primary_key: [id]
  unique:
  - name: t_code_key
    columns: [code]
  indexes:
  - name: t_ref_idx
    columns: [ref]
  foreign_keys:
  - name: t_ref_fkey
    columns: [ref]
    references:
      table: t
      columns: [id]
`
	cases := map[string]string{
		"column_default":         strings.Replace(base, `default: "'x'"`, `default: "'y'"`, 1),
		"column_identity":        strings.Replace(base, "identity: always", "identity: by_default", 1),
		"unique_columns":         strings.Replace(base, "columns: [code]", "columns: [code, ref]", 1),
		"unique_unexpected":      strings.Replace(base, "  unique:\n  - name: t_code_key\n    columns: [code]\n", "", 1),
		"index_unique":           strings.Replace(base, "columns: [ref]\n  foreign_keys", "columns: [ref]\n    unique: true\n  foreign_keys", 1),
		"foreign_key_definition": strings.Replace(base, "columns: [id]\n", "columns: [id]\n    on_delete: set_null\n", 1),
		"foreign_key_unexpected": strings.Replace(base, "  foreign_keys:\n  - name: t_ref_fkey\n    columns: [ref]\n    references:\n      table: t\n      columns: [id]\n", "", 1),
	}
	actual, err := Load(strings.NewReader(base))
	if err != nil {
		t.Fatal(err)
	}
	for kind, input := range cases {
		expected, err := Load(strings.NewReader(input))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		diff, err := Compare(expected, actual)
		if err != nil || len(diff) != 1 || diff[0].Kind != kind {
			t.Errorf("%s: %+v %v", kind, diff, err)
		}
	}
}

func TestNumericForeignKeyAcrossPrecisions(t *testing.T) {
	input := `version: 1
schema: public
tables:
- name: p
  columns:
  - name: n
    type: numeric(10,2)
  primary_key: [n]
- name: c
  columns:
  - name: n
    type: numeric(12,2)
  foreign_keys:
  - name: c_n_fkey
    columns: [n]
    references:
      table: p
      columns: [n]
`
	if _, err := Load(strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(strings.NewReader(strings.Replace(input, "type: numeric(12,2)", "type: text", 1))); err == nil {
		t.Fatal("numeric to text foreign key accepted")
	}
}
