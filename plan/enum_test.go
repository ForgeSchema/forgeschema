package plan

import (
	"strings"
	"testing"

	"github.com/ForgeSchema/forgeschema/schema"
)

func enumDoc(t *testing.T, y string) schema.Document {
	t.Helper()
	d, err := schema.Load(strings.NewReader("version: 1\nschema: shop\n" + y))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

const enumBase = `enums:
- name: status
  values: [a, c]
tables:
- name: t
  columns:
  - name: id
    type: integer
  - name: s
    type: status
    default: "'a'"
  - name: l
    type: status[]
    nullable: true
  primary_key: [id]
`

func TestEnumPlanSQL(t *testing.T) {
	empty := enumDoc(t, "tables: []\n")
	p, err := Build(empty, enumDoc(t, enumBase), nil)
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	if p.Operations[0].SQL != `CREATE TYPE "shop"."status" AS ENUM ('a', 'c');` {
		t.Fatalf("create_enum: %s", p.Operations[0].SQL)
	}
	if !strings.Contains(p.Operations[1].SQL, `"s" "shop"."status" DEFAULT 'a' NOT NULL`) || !strings.Contains(p.Operations[1].SQL, `"l" "shop"."status"[] NULL`) {
		t.Fatalf("create_table: %s", p.Operations[1].SQL)
	}
	next := enumDoc(t, strings.Replace(enumBase, "values: [a, c]", "values: [z, a, b, c, d]", 1))
	p, err = Build(enumDoc(t, enumBase), next, nil)
	if err != nil || p.Status != "ready" {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	got := []string{}
	for _, op := range p.Operations {
		got = append(got, op.SQL)
	}
	want := []string{
		`ALTER TYPE "shop"."status" ADD VALUE 'z' BEFORE 'a';`,
		`ALTER TYPE "shop"."status" ADD VALUE 'b' AFTER 'a';`,
		`ALTER TYPE "shop"."status" ADD VALUE 'd' AFTER 'c';`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEnumPlanBlocks(t *testing.T) {
	base := enumDoc(t, enumBase)
	for label, y := range map[string]string{
		"removed":    strings.Replace(enumBase, "values: [a, c]", "values: [a]", 1),
		"reordered":  strings.Replace(enumBase, "values: [a, c]", "values: [c, a]", 1),
		"new used":   strings.Replace(strings.Replace(enumBase, "values: [a, c]", "values: [a, c, n]", 1), `default: "'a'"`, `default: "'n'"`, 1),
		"to text":    strings.Replace(enumBase, "    type: status\n    default", "    type: text\n    default", 1),
		"array type": strings.Replace(enumBase, "type: status[]", "type: text[]", 1),
	} {
		p, err := Build(base, enumDoc(t, y), nil)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if p.Status != "blocked" {
			t.Errorf("%s: not blocked", label)
		}
	}
}

func TestViewEnumValueChecks(t *testing.T) {
	base := `enums:
- name: status
  values: [active]
- name: status_old
  values: [a, b]
tables:
- name: t
  columns:
  - name: id
    type: integer
  - name: s
    type: status
  - name: o
    type: status_old
  - name: l
    type: status[]
  primary_key: [id]
`
	src := enumDoc(t, base)
	grown := strings.Replace(base, "values: [active]", "values: [active, a, archived]", 1)
	for query, blocked := range map[string]bool{
		"SELECT id FROM t WHERE o = 'a'::status_old":           false,
		"SELECT id FROM t WHERE s = 'a'::status":               true,
		"SELECT id FROM t WHERE l = '{archived}'::status[]":    true,
		"SELECT id FROM t WHERE s = 'archived'::shop.status":   true,
		"SELECT id FROM t WHERE s = 'active'::status":          false,
		`SELECT id AS "x'y" FROM t WHERE s = 'active'::status`: false,
	} {
		dst := enumDoc(t, grown+"views:\n- name: v\n  query: \""+strings.ReplaceAll(query, `"`, `\"`)+"\"\n  depends_on: [t]\n")
		p, err := Build(src, dst, nil)
		if err != nil {
			t.Fatal(err)
		}
		if (p.Status == "blocked") != blocked {
			t.Errorf("%s: status %s %+v", query, p.Status, p.Blocked)
		}
	}
}
