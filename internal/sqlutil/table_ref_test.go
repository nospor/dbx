package sqlutil

import "testing"

func TestParseTableRef(t *testing.T) {
	tests := []struct {
		expr   string
		driver string
		schema string
		table  string
	}{
		{"users", "postgres", "public", "users"},
		{"public.users", "postgres", "public", "users"},
		{`"userSchema"."tbl"`, "postgres", "userSchema", "tbl"},
		{"orders", "mysql", "", "orders"},
		{"mydb.orders", "mysql", "mydb", "orders"},
		{"[dbo].[Orders]", "mssql", "dbo", "Orders"},
		{"[db].[dbo].[T]", "mssql", "dbo", "T"},
		{"widgets", "sqlite3", "", "widgets"},
	}
	for _, tt := range tests {
		s, tbl := ParseTableRef(tt.expr, tt.driver)
		if s != tt.schema || tbl != tt.table {
			t.Errorf("ParseTableRef(%q,%q) = (%q,%q); want (%q,%q)", tt.expr, tt.driver, s, tbl, tt.schema, tt.table)
		}
	}
}
