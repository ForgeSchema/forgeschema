package plan

import (
	"strings"

	"github.com/ForgeSchema/forgeschema/schema"
)

// viewOperations plans the views. A view holds no data, so a view that
// changes is dropped and created again, and so is every view that reads a
// view being dropped or a table whose columns this plan renames, converts or
// drops, since PostgreSQL refuses those changes while a view depends on them.
// Drops run before every other operation and creates after all of them.
func viewOperations(src, dst schema.Document, changedTables map[string]bool) (drops, creates []Operation) {
	before, after := map[string]schema.View{}, map[string]schema.View{}
	for _, v := range src.Views {
		before[v.Name] = v
	}
	for _, v := range dst.Views {
		after[v.Name] = v
	}
	// why holds, for each view to drop, the reason its explanation gives.
	why := map[string]string{}
	for _, v := range src.Views {
		want, ok := after[v.Name]
		switch {
		case !ok:
			why[v.Name] = "removed"
		case want.Query != v.Query || strings.Join(want.DependsOn, "\x00") != strings.Join(v.DependsOn, "\x00"):
			why[v.Name] = "changed"
		}
	}
	// Spread to the views that read what is dropped or changed, until nothing
	// more is added.
	for {
		grew := false
		for _, v := range src.Views {
			if why[v.Name] != "" {
				continue
			}
			var causes []string
			for _, dep := range v.DependsOn {
				if changedTables[dep] || why[dep] != "" {
					causes = append(causes, dep)
				}
			}
			if len(causes) > 0 {
				why[v.Name] = "reads " + strings.Join(causes, ", ")
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	order, _ := schema.ViewOrder(src.Views)
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		reason := why[name]
		if reason == "" {
			continue
		}
		explanation := "Drop view " + name + ", which the target no longer has. A view holds no data, so nothing is lost."
		switch {
		case reason == "changed":
			explanation = "Drop view " + name + " so it can be created again with its new definition. A view holds no data, so nothing is lost."
		case strings.HasPrefix(reason, "reads "):
			explanation = "Drop view " + name + ", which " + reason + ", so the changes there can go ahead; it is created again after them. A view holds no data, so nothing is lost."
		}
		drops = append(drops, Operation{Kind: "drop_view", Subject: name, SQL: "DROP VIEW " + qualified(src.Schema, name) + ";", Explanation: explanation})
	}
	order, _ = schema.ViewOrder(dst.Views)
	for _, name := range order {
		_, existed := before[name]
		if existed && why[name] == "" {
			continue
		}
		v := after[name]
		explanation := "Create view " + name
		if existed {
			explanation += " again"
		}
		if len(v.DependsOn) > 0 {
			explanation += ", which reads " + strings.Join(v.DependsOn, ", ")
		}
		explanation += ", after every other change. A view stores no data; PostgreSQL checks its query against what it reads."
		sql := "SET LOCAL search_path TO pg_catalog, " + quote(dst.Schema) + ";\nCREATE VIEW " + qualified(dst.Schema, name) + " AS " + v.Query + ";"
		creates = append(creates, Operation{Kind: "create_view", Subject: name, SQL: sql, Explanation: explanation})
	}
	return drops, creates
}

// tablesChangedForViews lists the tables whose columns the operations rename,
// convert or drop, or which they drop.
func tablesChangedForViews(phases ...[]Operation) map[string]bool {
	out := map[string]bool{}
	for _, ops := range phases {
		for _, op := range ops {
			switch op.Kind {
			case "rename_column", "alter_column_type", "convert_column_type", "drop_column", "drop_table":
				out[op.Table] = true
			}
		}
	}
	return out
}

// createdViews names the views the operations create.
func createdViews(ops []Operation) map[string]bool {
	out := map[string]bool{}
	for _, op := range ops {
		if op.Kind == "create_view" {
			out[op.Subject] = true
		}
	}
	return out
}
