package schema

import (
	"fmt"
	"sort"
	"strings"
)

// View is a plain view in the managed schema. Query is the text PostgreSQL
// prints for it (pg_get_viewdef with the managed schema on the search path),
// with whitespace outside literals collapsed and no trailing semicolon.
// DependsOn lists the tables and views of the schema the query reads.
type View struct {
	Name      string   `yaml:"name" json:"name"`
	Query     string   `yaml:"query" json:"query"`
	DependsOn []string `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
}

// MaxViewQuery is the longest view query accepted, in bytes.
const MaxViewQuery = 65536

// NormalizeViewQuery collapses whitespace outside quoted literals and
// identifiers and drops one trailing semicolon. It refuses anything that could
// end the CREATE VIEW statement or hide text from this scan: a semicolon
// elsewhere, comments, dollar quotes, backslashes and control characters.
func NormalizeViewQuery(raw string) (string, error) {
	if len(raw) > MaxViewQuery {
		return "", fmt.Errorf("query is longer than %d bytes", MaxViewQuery)
	}
	var b strings.Builder
	space := false
	var quote byte // ' or " while inside a literal or quoted identifier
	end := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == '\\' {
			return "", fmt.Errorf("query may not contain a backslash")
		}
		if c < 0x20 && c != ' ' && c != '\t' && c != '\n' && c != '\r' || c == 0x7f {
			return "", fmt.Errorf("query may not contain control characters")
		}
		if quote != 0 {
			if c == '\t' || c == '\n' || c == '\r' {
				return "", fmt.Errorf("query may not contain tabs or line breaks inside quotes")
			}
			b.WriteByte(c)
			if c == quote {
				if i+1 < len(raw) && raw[i+1] == quote {
					b.WriteByte(c)
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if end {
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				return "", fmt.Errorf("query must be one statement: a semicolon may only end it")
			}
			continue
		}
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			space = true
			continue
		case c == ';':
			end = true
			continue
		case c == '$':
			return "", fmt.Errorf("query may not contain $ (dollar quotes or parameters)")
		case c == '-' && i+1 < len(raw) && raw[i+1] == '-', c == '/' && i+1 < len(raw) && raw[i+1] == '*':
			return "", fmt.Errorf("query may not contain comments")
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteByte(c)
		if c == '\'' || c == '"' {
			quote = c
		}
	}
	if quote != 0 {
		return "", fmt.Errorf("query has an unterminated quote")
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("query is required")
	}
	return b.String(), nil
}

// normalizeViews validates the document's views against its tables and
// orders them; reserve and claim register each name with the relation and
// type namespaces Normalize keeps.
func normalizeViews(d *Document, reserve func(name, what string) error, claim func(name, what string) error) error {
	tables := map[string]bool{}
	for _, t := range d.Tables {
		tables[t.Name] = true
	}
	views := map[string]*View{}
	for vi := range d.Views {
		v := &d.Views[vi]
		if !validName(v.Name) {
			return fmt.Errorf("invalid or reserved view name %q", v.Name)
		}
		if err := reserve(v.Name, "view"); err != nil {
			return err
		}
		if err := claim(v.Name, "view"); err != nil {
			return err
		}
		q, err := NormalizeViewQuery(v.Query)
		if err != nil {
			return fmt.Errorf("view %s: %w", v.Name, err)
		}
		v.Query = q
		views[v.Name] = v
	}
	for _, v := range views {
		seen := map[string]bool{}
		for _, n := range v.DependsOn {
			if n == v.Name {
				return fmt.Errorf("view %s: depends_on names the view itself", v.Name)
			}
			if !tables[n] && views[n] == nil {
				return fmt.Errorf("view %s: depends_on %q is not a table or view of the schema", v.Name, n)
			}
			if seen[n] {
				return fmt.Errorf("view %s: depends_on repeats %q", v.Name, n)
			}
			seen[n] = true
		}
		sort.Strings(v.DependsOn)
		if len(v.DependsOn) == 0 {
			v.DependsOn = nil
		}
	}
	if _, err := ViewOrder(d.Views); err != nil {
		return err
	}
	sort.Slice(d.Views, func(i, j int) bool { return d.Views[i].Name < d.Views[j].Name })
	if len(d.Views) == 0 {
		d.Views = nil
	}
	return nil
}

// ViewOrder returns view names so that every view comes after the views it
// depends on, in a fixed order for a given set. It fails on a cycle.
func ViewOrder(views []View) ([]string, error) {
	byName := map[string]View{}
	for _, v := range views {
		byName[v.Name] = v
	}
	names := make([]string, 0, len(views))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	state := map[string]int{} // 1 visiting, 2 done
	var out []string
	var visit func(n string, path []string) error
	visit = func(n string, path []string) error {
		switch state[n] {
		case 1:
			return fmt.Errorf("views depend on each other in a cycle: %s", strings.Join(append(path, n), " -> "))
		case 2:
			return nil
		}
		state[n] = 1
		for _, dep := range byName[n].DependsOn {
			if _, ok := byName[dep]; ok {
				if err := visit(dep, append(path, n)); err != nil {
					return err
				}
			}
		}
		state[n] = 2
		out = append(out, n)
		return nil
	}
	for _, n := range names {
		if err := visit(n, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}
