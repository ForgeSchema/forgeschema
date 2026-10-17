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
	Kind       string `json:"kind"`
	Table      string `json:"table"`
	Column     string `json:"column,omitempty"`
	Index      string `json:"index,omitempty"`
	Constraint string `json:"constraint,omitempty"`
	Expected   string `json:"expected"`
	Actual     string `json:"actual"`
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
	add := func(d Difference) { differences = append(differences, d) }
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
			add(Difference{Kind: "table_missing", Table: t.Name, Expected: "present", Actual: "absent"})
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
				add(Difference{Kind: "column_missing", Table: t.Name, Column: c.Name, Expected: "present", Actual: "absent"})
				continue
			}
			if c.Type != observed.Type {
				add(Difference{Kind: "column_type", Table: t.Name, Column: c.Name, Expected: c.Type, Actual: observed.Type})
			}
			if c.Nullable != observed.Nullable {
				add(Difference{Kind: "column_nullability", Table: t.Name, Column: c.Name, Expected: strconv.FormatBool(c.Nullable), Actual: strconv.FormatBool(observed.Nullable)})
			}
			if c.Default != observed.Default {
				add(Difference{Kind: "column_default", Table: t.Name, Column: c.Name, Expected: orNone(c.Default), Actual: orNone(observed.Default)})
			}
			if c.Identity != observed.Identity {
				add(Difference{Kind: "column_identity", Table: t.Name, Column: c.Name, Expected: orNone(c.Identity), Actual: orNone(observed.Identity)})
			}
		}
		for _, c := range live.Columns {
			if _, ok := ec[c.Name]; !ok {
				add(Difference{Kind: "column_unexpected", Table: t.Name, Column: c.Name, Expected: "absent", Actual: "present"})
			}
		}
		if columnList(t.PrimaryKey) != columnList(live.PrimaryKey) {
			add(Difference{Kind: "primary_key_columns", Table: t.Name, Expected: columnList(t.PrimaryKey), Actual: columnList(live.PrimaryKey)})
		}
		eu, au := map[string]Unique{}, map[string]Unique{}
		for _, u := range t.Unique {
			eu[u.Name] = u
		}
		for _, u := range live.Unique {
			au[u.Name] = u
		}
		for _, u := range t.Unique {
			observed, ok := au[u.Name]
			if !ok {
				add(Difference{Kind: "unique_missing", Table: t.Name, Constraint: u.Name, Expected: columnList(u.Columns), Actual: "absent"})
			} else if columnList(u.Columns) != columnList(observed.Columns) {
				add(Difference{Kind: "unique_columns", Table: t.Name, Constraint: u.Name, Expected: columnList(u.Columns), Actual: columnList(observed.Columns)})
			}
		}
		for _, u := range live.Unique {
			if _, ok := eu[u.Name]; !ok {
				add(Difference{Kind: "unique_unexpected", Table: t.Name, Constraint: u.Name, Expected: "absent", Actual: columnList(u.Columns)})
			}
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
				add(Difference{Kind: "index_missing", Table: t.Name, Index: idx.Name, Expected: columnList(idx.Columns), Actual: "absent"})
				continue
			}
			if columnList(idx.Columns) != columnList(observed.Columns) {
				add(Difference{Kind: "index_columns", Table: t.Name, Index: idx.Name, Expected: columnList(idx.Columns), Actual: columnList(observed.Columns)})
			}
			if idx.Unique != observed.Unique {
				add(Difference{Kind: "index_unique", Table: t.Name, Index: idx.Name, Expected: strconv.FormatBool(idx.Unique), Actual: strconv.FormatBool(observed.Unique)})
			}
		}
		for _, idx := range live.Indexes {
			if _, ok := ei[idx.Name]; !ok {
				add(Difference{Kind: "index_unexpected", Table: t.Name, Index: idx.Name, Expected: "absent", Actual: columnList(idx.Columns)})
			}
		}
		ef, af := map[string]ForeignKey{}, map[string]ForeignKey{}
		for _, fk := range t.ForeignKeys {
			ef[fk.Name] = fk
		}
		for _, fk := range live.ForeignKeys {
			af[fk.Name] = fk
		}
		for _, fk := range t.ForeignKeys {
			observed, ok := af[fk.Name]
			if !ok {
				add(Difference{Kind: "foreign_key_missing", Table: t.Name, Constraint: fk.Name, Expected: ForeignKeyDefinition(fk), Actual: "absent"})
			} else if ForeignKeyDefinition(fk) != ForeignKeyDefinition(observed) {
				add(Difference{Kind: "foreign_key_definition", Table: t.Name, Constraint: fk.Name, Expected: ForeignKeyDefinition(fk), Actual: ForeignKeyDefinition(observed)})
			}
		}
		for _, fk := range live.ForeignKeys {
			if _, ok := ef[fk.Name]; !ok {
				add(Difference{Kind: "foreign_key_unexpected", Table: t.Name, Constraint: fk.Name, Expected: "absent", Actual: ForeignKeyDefinition(fk)})
			}
		}
	}
	for _, t := range a.Tables {
		if _, ok := em[t.Name]; !ok {
			add(Difference{Kind: "table_unexpected", Table: t.Name, Expected: "absent", Actual: "present"})
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
		if a.Constraint != b.Constraint {
			return a.Constraint < b.Constraint
		}
		return a.Kind < b.Kind
	})
	return differences, nil
}

// ForeignKeyDefinition is a compact, deterministic description of a foreign key.
func ForeignKeyDefinition(fk ForeignKey) string {
	s := columnList(fk.Columns) + " references " + fk.References.Table + columnList(fk.References.Columns)
	if fk.OnDelete != "" {
		s += " on_delete " + fk.OnDelete
	}
	if fk.OnUpdate != "" {
		s += " on_update " + fk.OnUpdate
	}
	return s
}
func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
func columnList(ns []string) string {
	b, _ := json.Marshal(append([]string{}, ns...))
	return string(b)
}
