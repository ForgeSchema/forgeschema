package postgres_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ForgeSchema/forgeschema/plan"
	"github.com/ForgeSchema/forgeschema/postgres"
	"github.com/ForgeSchema/forgeschema/schema"
)

// SQL is executed by this test harness only. The production CLI has no apply
// command. Reinspection checks that generated SQL actually reaches the target.
func TestGeneratedPlanSQLAgainstPostgres(t *testing.T) {
	db := database(t)
	name := isolated(t, db)
	fixture, err := os.ReadFile("../testdata/postgres/001-customers.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, db, strings.ReplaceAll(string(fixture), "public.", name+"."))
	exec(t, db, "CREATE INDEX customers_name_idx ON "+name+".customers(customer_name,id)")
	src, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := schema.Normalize(src)
	if err != nil {
		t.Fatal(err)
	}
	for i := range dst.Tables[0].Columns {
		switch dst.Tables[0].Columns[i].Name {
		case "id":
			dst.Tables[0].Columns[i].Name = "customer_id"
		case "customer_name":
			dst.Tables[0].Columns[i].Name = "full_name"
		}
	}
	dst.Tables[0].PrimaryKey = []string{"customer_id"}
	for i := range dst.Tables[0].Indexes {
		if dst.Tables[0].Indexes[i].Name == "customers_name_idx" {
			dst.Tables[0].Indexes[i].Columns = []string{"full_name", "customer_id"}
		}
	}
	dst.Tables[0].Columns = append(dst.Tables[0].Columns, schema.Column{Name: "notes", Type: "text", Nullable: true})
	dst.Tables[0].Indexes = append(dst.Tables[0].Indexes, schema.Index{Name: "customers_notes_idx", Columns: []string{"notes"}})
	dst.Tables = append(dst.Tables, schema.Table{Name: "select", Columns: []schema.Column{{Name: "from", Type: "integer"}}, PrimaryKey: []string{"from"}, Indexes: []schema.Index{{Name: "select_from_idx", Columns: []string{"from"}}}})
	p, err := plan.Build(src, dst, []plan.Rename{{Table: "customers", From: "id", To: "customer_id"}, {Table: "customers", From: "customer_name", To: "full_name"}})
	if err != nil || p.Status != "ready" {
		t.Fatalf("plan failed %+v %v", p, err)
	}
	if err = p.Validate(); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, op := range p.Operations {
		if _, err = tx.Exec(op.SQL); err != nil {
			t.Fatalf("%s: %v", op.SQL, err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	actual, err := postgres.Inspect(context.Background(), db, name)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := dst.CanonicalJSON()
	got, _ := actual.CanonicalJSON()
	if !bytes.Equal(want, got) {
		t.Fatalf("generated SQL missed target\nwant %s\ngot %s", want, got)
	}
	rows, err := db.Query("SELECT customer_id,full_name,notes IS NULL FROM " + name + ".customers ORDER BY customer_id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	expected := []string{"Ada Example", "Dana Example"}
	n := 0
	for rows.Next() {
		var id int
		var fullName string
		var nullNotes bool
		if err = rows.Scan(&id, &fullName, &nullNotes); err != nil {
			t.Fatal(err)
		}
		if n >= len(expected) || id != n+1 || fullName != expected[n] || !nullNotes {
			t.Fatal("row data changed during generated rename/additions")
		}
		n++
	}
	if err = rows.Err(); err != nil || n != 2 {
		t.Fatalf("data mismatch %d %v", n, err)
	}
	t.Logf("Executed %d generated operations; snapshot matches target and both original rows are intact", len(p.Operations))
}
