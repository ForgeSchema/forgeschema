package plan

import "testing"

func TestEnumCasts(t *testing.T) {
	q := `SELECT id FROM t WHERE o = 'a'::status_old AND s = 'it''s'::status AND l = '{x,y}'::shop."order"[] AND m = 'z'::rv2.mood AND "we'ird" = 'q'::text`
	got := enumCasts(q, "shop")
	want := []enumCast{{"a", "status_old", false}, {"it's", "status", false}, {"{x,y}", "order", true}, {"q", "text", false}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cast %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
