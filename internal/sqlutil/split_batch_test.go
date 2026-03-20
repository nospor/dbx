package sqlutil

import (
	"reflect"
	"testing"
)

func TestSplitSemicolonStatements(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"single", "DELETE FROM a WHERE id=1", []string{"DELETE FROM a WHERE id=1"}},
		{"two", "DELETE FROM a; DELETE FROM b", []string{"DELETE FROM a", " DELETE FROM b"}},
		{"semi in string", "DELETE FROM a WHERE x='a;b'", []string{"DELETE FROM a WHERE x='a;b'"}},
		{"double single quote", "DELETE FROM a WHERE x='a'';b'", []string{"DELETE FROM a WHERE x='a'';b'"}},
		{"double quoted ident", `DELETE FROM "t;bl"`, []string{`DELETE FROM "t;bl"`}},
		{"paren semi", "DELETE FROM (SELECT 1)", []string{"DELETE FROM (SELECT 1)"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := SplitSemicolonStatements(tc.sql)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v want %#v", got, tc.want)
			}
		})
	}
}

func TestSplitExecBatchDeleteUpdate(t *testing.T) {
	t.Parallel()
	st, ok := SplitExecBatchDeleteUpdate("DELETE FROM a; UPDATE b SET x=1")
	if !ok || len(st) != 2 {
		t.Fatalf("got ok=%v st=%#v", ok, st)
	}
	_, ok = SplitExecBatchDeleteUpdate("DELETE FROM a; SELECT 1")
	if ok {
		t.Fatal("expected false for mixed SELECT")
	}
	_, ok = SplitExecBatchDeleteUpdate("DELETE FROM a; INSERT INTO b VALUES(1)")
	if ok {
		t.Fatal("expected false for INSERT")
	}
	st, ok = SplitExecBatchDeleteUpdate("DELETE FROM a WHERE x='x'; /*c*/ ; DELETE FROM b")
	if !ok || len(st) != 2 {
		t.Fatalf("comment between: ok=%v %#v", ok, st)
	}
}
