package schema

import "testing"

func probeColumns() map[string]Column {
	cols := map[string]Column{}
	for name, typ := range map[string]string{
		"i": "integer", "s": "smallint", "b": "bigint", "n": "numeric(10,2)", "n2": "numeric", "r": "real", "d": "double precision",
		"tx": "text", "in": "integer", "vc": "character varying(20)", "ch": "character(3)", "dt": "date", "ts": "timestamp with time zone", "bo": "boolean",
	} {
		cols[name] = Column{Name: name, Type: typ, Nullable: true}
	}
	return cols
}

func TestNormalizeCheck(t *testing.T) {
	cols := probeColumns()
	good := map[string]string{
		"i>0":                          "i > 0",
		"i != 7":                       "i <> 7",
		"I > 0":                        "",
		"s >= -5 and i < 10":           "s >= -5 AND i < 10",
		"(i > 0) AND (i < 10 AND b>1)": "i > 0 AND i < 10 AND b > 1",
		"tx in ('a','b')":              "tx IN ('a', 'b')",
		"ch IN ('abc')":                "ch = 'abc'",
		"char_length(tx) <= 100":       "char_length(tx) <= 100",
		"vc <> tx":                     "vc <> tx",
		"n < 99.5":                     "n < 99.5",
		"bo = TRUE":                    "bo = true",
		`"i" > 0`:                      "i > 0",
		`"in" > 0`:                     `"in" > 0`,
		"n < n2":                       "n < n2",
		"dt <= dt":                     "dt <= dt",
	}
	for in, want := range good {
		got, err := NormalizeCheck(cols, in)
		if want == "" {
			if err == nil {
				t.Errorf("%q: accepted as %q", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"", "i", "i > 0 OR i < 5", "i > 1.5", "i > 2147483648", "n > 99.555", "tx > 1", "i > '1'", "i > 0::integer",
		"0 < i", "missing > 0", "r > 0", "dt < ts", "i < n", "char_length(i) > 0", "char_length(tx) > tx",
		"tx = 'a\\b'", "tx = 'open", "i > 0 AND", "i > 0; DROP TABLE x", "tx = now()", "i IS NOT NULL", "vc = 'this is longer than twenty'",
		"i = ANY (ARRAY[1,2])", "i > -0",
	} {
		if got, err := NormalizeCheck(cols, bad); err == nil {
			t.Errorf("%q accepted as %q", bad, got)
		}
	}
}

// The catalog forms are pg_get_constraintdef output from PostgreSQL 16 for
// the canonical expressions beside them.
func TestCheckFromCatalog(t *testing.T) {
	cols := probeColumns()
	cases := map[string]string{
		"CHECK ((i > 0))":                                  "i > 0",
		"CHECK ((s >= '-5'::integer))":                     "s >= -5",
		"CHECK ((b <> 7))":                                 "b <> 7",
		"CHECK ((n >= (0)::numeric))":                      "n >= 0",
		"CHECK ((n < 99.5))":                               "n < 99.5",
		"CHECK ((n > '-1.25'::numeric))":                   "n > -1.25",
		"CHECK ((tx = ANY (ARRAY['a'::text, 'b'::text])))": "tx IN ('a', 'b')",
		"CHECK (((vc)::text = ANY ((ARRAY['a'::character varying, 'it''s'::character varying])::text[])))": "vc IN ('a', 'it''s')",
		"CHECK ((ch = 'abc'::bpchar))":                        "ch = 'abc'",
		"CHECK ((tx <> ''::text))":                            "tx <> ''",
		"CHECK (((vc)::text = 'x'::text))":                    "vc = 'x'",
		"CHECK ((char_length(tx) <= 100))":                    "char_length(tx) <= 100",
		"CHECK ((char_length((vc)::text) > 0))":               "char_length(vc) > 0",
		"CHECK ((char_length(ch) = 3))":                       "char_length(ch) = 3",
		"CHECK (((i > 0) AND (i < 10)))":                      "i > 0 AND i < 10",
		"CHECK ((i < b))":                                     "i < b",
		"CHECK (((vc)::text <> tx))":                          "vc <> tx",
		"CHECK ((n < n2))":                                    "n < n2",
		"CHECK ((i = ANY (ARRAY[1, 2, 3])))":                  "i IN (1, 2, 3)",
		"CHECK ((n = ANY (ARRAY[(1)::numeric, 2.5])))":        "n IN (1, 2.5)",
		"CHECK ((s = ANY (ARRAY[1, 2])))":                     "s IN (1, 2)",
		"CHECK (((i > 0) AND ((s > 0) AND (b > 0))))":         "i > 0 AND s > 0 AND b > 0",
		"CHECK ((b > '-9223372036854775808'::bigint))":        "b > -9223372036854775808",
		"CHECK ((b = ANY (ARRAY[(1)::bigint, (2)::bigint])))": "b IN (1, 2)",
		"CHECK ((bo = true))":                                 "bo = true",
		// After a varchar column is widened or becomes text.
		"CHECK ((tx = ANY (ARRAY[('a'::character varying)::text, ('b'::character varying)::text])))": "tx IN ('a', 'b')",
		"CHECK (((vc)::text = ANY (ARRAY[('a'::character varying)::text])))":                         "vc = 'a'",
		"CHECK ((\"i\" > 0))": "i > 0",
	}
	for def, want := range cases {
		got, ok := CheckFromCatalog(cols, def)
		if !ok || got != want {
			t.Errorf("%s: got %q %v, want %q", def, got, ok, want)
		}
		if again, err := NormalizeCheck(cols, got); err != nil || again != got {
			t.Errorf("%s: canonical form %q does not round-trip: %q %v", def, got, again, err)
		}
	}
	for _, def := range []string{
		"CHECK ((r > (0)::double precision))", "CHECK ((d <= (1.5)::double precision))", "CHECK ((dt < ts))",
		"CHECK ((i > '2147483648'::bigint))", "CHECK ((r = ANY (ARRAY[(1)::real, (2)::real])))",
		"CHECK (((i > 0) OR (i < '-10'::integer)))", "CHECK ((i IS NOT NULL))", "CHECK ((dt = (ts)::date))",
		"CHECK ((i > (1.5)::integer))", "CHECK ((n > (1.5)::integer))", "CHECK ((tx = 'a'::bpchar))", "CHECK ((ch = 'abc'::text))", "CHECK ((tx = ('a '::bpchar)::text))", "CHECK ((vc = ('a'::character varying(1))::text))",
		"CHECK ((tx)::text = 'a'::text)", "CHECK (i > 0) NO INHERIT", "CHECK ((lower(tx) = 'a'::text))",
	} {
		if got, ok := CheckFromCatalog(cols, def); ok {
			t.Errorf("%s accepted as %q", def, got)
		}
	}
}

func TestCheckSQLAndRename(t *testing.T) {
	cols := map[string]Column{"order": {Name: "order", Type: "integer"}, "label": {Name: "label", Type: "text"}}
	expr, err := NormalizeCheck(cols, `"order" > 0 AND char_length(label) IN (1, 2)`)
	if err != nil {
		t.Fatal(err)
	}
	if got := CheckSQL(cols, expr); got != `"order" > 0 AND char_length("label") IN (1, 2)` {
		t.Fatalf("sql %s", got)
	}
	if got := RenameCheckColumns(cols, expr, map[string]string{"label": "title"}); got != "order > 0 AND char_length(title) IN (1, 2)" {
		t.Fatalf("rename %s", got)
	}
}
