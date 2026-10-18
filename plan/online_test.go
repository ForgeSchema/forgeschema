// Copyright 2026 ForgeSchema
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ForgeSchema/forgeschema/schema"
)

func onlineDocs(t *testing.T) (schema.Document, schema.Document) {
	src := doc(t, base)
	dst := doc(t, base+`- name: orders
  columns:
  - name: id
    type: bigint
  - name: customer_id
    type: bigint
  primary_key: [id]
  indexes:
  - name: orders_customer_idx
    columns: [customer_id]
  foreign_keys:
  - name: orders_customer_fkey
    columns: [customer_id]
    references: {table: customers, columns: [id]}
`)
	c := &dst.Tables[0]
	c.Columns = append(c.Columns, schema.Column{Name: "code", Type: "text", Nullable: true})
	c.Indexes = append(c.Indexes, schema.Index{Name: "customers_name_idx", Columns: []string{"customer_name"}})
	c.Unique = append(c.Unique, schema.Unique{Name: "customers_code_key", Columns: []string{"code"}})
	return src, dst
}

func TestOnlinePlanPhases(t *testing.T) {
	src, dst := onlineDocs(t)
	p, err := BuildWith(src, dst, nil, Options{Online: true})
	if err != nil || p.Status != "ready" || !p.Online {
		t.Fatalf("%+v %v", p.Blocked, err)
	}
	first, concurrent, final := p.Phases()
	if len(first)+len(concurrent)+len(final) != len(p.Operations) {
		t.Fatal("phases lose operations")
	}
	var built []string
	for _, op := range concurrent {
		if op.Kind != ConcurrentIndex || !strings.Contains(op.SQL, "INDEX CONCURRENTLY") {
			t.Fatalf("bad concurrent op %+v", op)
		}
		built = append(built, op.Subject)
	}
	// Only indexes on the existing table are built concurrently; the new
	// table's index is created with it.
	if !reflect.DeepEqual(built, []string{"customers_name_idx", "customers_code_key"}) && !reflect.DeepEqual(built, []string{"customers_code_key", "customers_name_idx"}) {
		t.Fatalf("built %v", built)
	}
	kinds := []string{}
	for _, op := range final {
		kinds = append(kinds, op.Kind)
	}
	if len(kinds) != 2 || kinds[0] != "attach_unique" || kinds[1] != "add_foreign_key" {
		t.Fatalf("final %v", kinds)
	}
	for _, op := range first {
		if strings.Contains(op.SQL, "customers_name_idx") || strings.Contains(op.SQL, "customers_code_key") {
			t.Fatalf("built index in the first transaction: %s", op.SQL)
		}
	}
	afterFirst, beforeFinal, err := p.Intermediate()
	if err != nil {
		t.Fatal(err)
	}
	if len(afterFirst.Tables[0].Indexes) != 1 || len(afterFirst.Tables[0].Unique) != 0 {
		t.Fatalf("after first %+v", afterFirst.Tables[0])
	}
	if len(beforeFinal.Tables[0].Indexes) != 3 || len(beforeFinal.Tables[1].ForeignKeys) != 0 {
		t.Fatalf("before final %+v", beforeFinal.Tables)
	}
	if err = p.Validate(); err != nil {
		t.Fatal(err)
	}
	summary, err := p.Summary()
	if err != nil || !strings.Contains(summary, "three phases") {
		t.Fatalf("%s %v", summary, err)
	}
}

func TestPlainPlanUnchangedByOnlineOption(t *testing.T) {
	src, dst := onlineDocs(t)
	plain, err := Build(src, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Online {
		t.Fatal("plain plan is online")
	}
	for _, op := range plain.Operations {
		if op.Kind == ConcurrentIndex || strings.Contains(op.SQL, "CONCURRENTLY") {
			t.Fatalf("plain plan builds concurrently: %s", op.SQL)
		}
	}
	if _, concurrent, _ := plain.Phases(); len(concurrent) != 0 {
		t.Fatal("plain plan has a concurrent phase")
	}
	// Flipping the flag on a saved plan makes it fail validation.
	plain.Online = true
	if err = plain.Validate(); err == nil {
		t.Fatal("tampered online flag accepted")
	}
}
