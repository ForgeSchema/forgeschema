package schema

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// Difference describes observed structural drift, not a proposed migration.
// Expected/Actual values are strings; ordered keys/index columns use JSON array
// strings. Missing/extra objects use present/absent markers.
type Difference struct {
	Kind     string `json:"kind"`
	Table    string `json:"table"`
	Column   string `json:"column,omitempty"`
	Index    string `json:"index,omitempty"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// Compare reports supported structural differences without inferring renames,
// generating SQL or applying migration policy. Inputs are copied/normalized.
func Compare(expected, actual Document) ([]Difference, error) {
	e, err := Normalize(expected)
	if err != nil {
		return nil, fmt.Errorf("expected: %w", err)
	}
	a, err := Normalize(actual)
	if err != nil {
		return nil, fmt.Errorf("actual: %w", err)
	}
	if e.Schema != a.Schema {
		return nil, fmt.Errorf("verification schemas must match")
	}
	differences := []Difference{}
	em, am := map[string]Table{}, map[string]Table{}
	for _, t := range e.Tables {
		em[t.Name] = t
	}
	for _, t := range a.Tables {
		am[t.Name] = t
	}
	for _, t := range e.Tables {
		live, ok := am[t.Name]
		if !ok {
			differences = append(differences, Difference{Kind: "table_missing", Table: t.Name, Expected: "present", Actual: "absent"})
			continue
		}
		ec, ac := map[string]Column{}, map[string]Column{}
		for _, c := range t.Columns {
			ec[c.Name] = c
		}
		for _, c := range live.Columns {
			ac[c.Name] = c
		}
		for _, c := range t.Columns {
			observed, ok := ac[c.Name]
			if !ok {
				differences = append(differences, Difference{Kind: "column_missing", Table: t.Name, Column: c.Name, Expected: "present", Actual: "absent"})
				continue
			}
			if c.Type != observed.Type {
				differences = append(differences, Difference{Kind: "column_type", Table: t.Name, Column: c.Name, Expected: c.Type, Actual: observed.Type})
			}
			if c.Nullable != observed.Nullable {
				differences = append(differences, Difference{Kind: "column_nullability", Table: t.Name, Column: c.Name, Expected: strconv.FormatBool(c.Nullable), Actual: strconv.FormatBool(observed.Nullable)})
			}
		}
		for _, c := range live.Columns {
			if _, ok := ec[c.Name]; !ok {
				differences = append(differences, Difference{Kind: "column_unexpected", Table: t.Name, Column: c.Name, Expected: "absent", Actual: "present"})
			}
		}
		if columnList(t.PrimaryKey) != columnList(live.PrimaryKey) {
			differences = append(differences, Difference{Kind: "primary_key_columns", Table: t.Name, Expected: columnList(t.PrimaryKey), Actual: columnList(live.PrimaryKey)})
		}
		ei, ai := map[string]Index{}, map[string]Index{}
		for _, idx := range t.Indexes {
			ei[idx.Name] = idx
		}
		for _, idx := range live.Indexes {
			ai[idx.Name] = idx
		}
		for _, idx := range t.Indexes {
			observed, ok := ai[idx.Name]
			if !ok {
				differences = append(differences, Difference{Kind: "index_missing", Table: t.Name, Index: idx.Name, Expected: columnList(idx.Columns), Actual: "absent"})
			} else if columnList(idx.Columns) != columnList(observed.Columns) {
				differences = append(differences, Difference{Kind: "index_columns", Table: t.Name, Index: idx.Name, Expected: columnList(idx.Columns), Actual: columnList(observed.Columns)})
			}
		}
		for _, idx := range live.Indexes {
			if _, ok := ei[idx.Name]; !ok {
				differences = append(differences, Difference{Kind: "index_unexpected", Table: t.Name, Index: idx.Name, Expected: "absent", Actual: columnList(idx.Columns)})
			}
		}
	}
	for _, t := range a.Tables {
		if _, ok := em[t.Name]; !ok {
			differences = append(differences, Difference{Kind: "table_unexpected", Table: t.Name, Expected: "absent", Actual: "present"})
		}
	}
	sort.Slice(differences, func(i, j int) bool {
		a, b := differences[i], differences[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.Index != b.Index {
			return a.Index < b.Index
		}
		return a.Kind < b.Kind
	})
	return differences, nil
}
func columnList(ns []string) string {
	b, _ := json.Marshal(append([]string{}, ns...))
	return string(b)
}
