package schema

import (
	"reflect"
	"strings"
	"testing"
)

func compareFixture(t *testing.T) Document {
	t.Helper()
	d, err := Load(strings.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestCompareMatchAndOrdering(t *testing.T) {
	e := compareFixture(t)
	a := compareFixture(t)
	a.Tables[0].Columns[0], a.Tables[0].Columns[1] = a.Tables[0].Columns[1], a.Tables[0].Columns[0]
	for i := range a.Tables[0].Columns {
		if a.Tables[0].Columns[i].Type == "bigint" {
			a.Tables[0].Columns[i].Type = "int8"
		}
	}
	diff, err := Compare(e, a)
	if err != nil || diff == nil || len(diff) != 0 {
		t.Fatalf("%+v %v", diff, err)
	}
}
func TestCompareDifferenceKinds(t *testing.T) {
	cases := map[string]func(*Document){
		"table_missing": func(d *Document) { d.Tables = []Table{} },
		"table_unexpected": func(d *Document) {
			d.Tables = append(d.Tables, Table{Name: "extra", Columns: []Column{{Name: "id", Type: "integer"}}})
		},
		"column_missing": func(d *Document) { d.Tables[0].Columns = d.Tables[0].Columns[1:]; d.Tables[0].Indexes = nil },
		"column_unexpected": func(d *Document) {
			d.Tables[0].Columns = append(d.Tables[0].Columns, Column{Name: "extra", Type: "text", Nullable: true})
		},
		"column_type":         func(d *Document) { d.Tables[0].Columns[0].Type = "integer" },
		"column_nullability":  func(d *Document) { d.Tables[0].Columns[0].Nullable = false },
		"primary_key_columns": func(d *Document) { d.Tables[0].PrimaryKey = nil },
		"index_missing":       func(d *Document) { d.Tables[0].Indexes = nil },
		"index_unexpected": func(d *Document) {
			d.Tables[0].Indexes = append(d.Tables[0].Indexes, Index{Name: "extra_idx", Columns: []string{"id"}})
		},
		"index_columns": func(d *Document) { d.Tables[0].Indexes[0].Columns = []string{"id"} },
	}
	for kind, mutate := range cases {
		t.Run(kind, func(t *testing.T) {
			e := compareFixture(t)
			a := compareFixture(t)
			mutate(&a)
			diff, err := Compare(e, a)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, v := range diff {
				found = found || v.Kind == kind
			}
			if !found {
				t.Fatalf("missing %s in %+v", kind, diff)
			}
			again, err := Compare(e, a)
			if err != nil || !reflect.DeepEqual(diff, again) {
				t.Fatal("nondeterministic report")
			}
		})
	}
}
func TestCompareOrderedKeysAndIndexes(t *testing.T) {
	e := compareFixture(t)
	e.Tables[0].Columns[0].Nullable = false
	e.Tables[0].PrimaryKey = []string{"id", "email"}
	e.Tables[0].Indexes[0].Columns = []string{"email", "id"}
	a, _ := Normalize(e)
	a.Tables[0].PrimaryKey = []string{"email", "id"}
	a.Tables[0].Indexes[0].Columns = []string{"id", "email"}
	diff, err := Compare(e, a)
	if err != nil || len(diff) != 2 {
		t.Fatalf("ordered definitions: %+v %v", diff, err)
	}
}
func TestCompareInvalidInputs(t *testing.T) {
	e := compareFixture(t)
	a := compareFixture(t)
	a.Schema = "other"
	if _, err := Compare(e, a); err == nil {
		t.Fatal("accepted different schemas")
	}
	a = e
	a.Version = 0
	if _, err := Compare(e, a); err == nil {
		t.Fatal("accepted invalid actual")
	}
	if _, err := Compare(a, e); err == nil {
		t.Fatal("accepted invalid expected")
	}
}
