package sqlutil

import "testing"

func TestInsertForRows(t *testing.T) {
	sql, err := InsertForRows("postgres", `public."users"`, []string{"id", "name"}, [][]string{
		{"1", "alice"},
		{"2", "bob"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `INSERT INTO public."users" ("id", "name") VALUES (1, 'alice');
INSERT INTO public."users" ("id", "name") VALUES (2, 'bob');`
	if sql != want {
		t.Fatalf("got:\n%s\nwant:\n%s", sql, want)
	}
}

func TestInsertForRows_nullCell(t *testing.T) {
	sql, err := InsertForRows("postgres", "t", []string{"a"}, [][]string{{"NULL"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `INSERT INTO t ("a") VALUES (NULL);`; sql != want {
		t.Fatalf("got %q want %q", sql, want)
	}
}

func TestInsertForRows_mysqlBacktick(t *testing.T) {
	sql, err := InsertForRows("mysql", "`tbl`", []string{"x"}, [][]string{{"a'b"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "INSERT INTO `tbl` (`x`) VALUES ('a''b');"; sql != want {
		t.Fatalf("got %q want %q", sql, want)
	}
}
