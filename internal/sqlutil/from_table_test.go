package sqlutil

import "testing"

func TestTableFromSimpleSelect(t *testing.T) {
	tests := []struct {
		sql  string
		want string
		ok   bool
	}{
		{"SELECT * FROM users LIMIT 10", "users", true},
		{"select *\nfrom   my_table where x=1", "my_table", true},
		{"SELECT * FROM public.orders", "public.orders", true},
		{`SELECT * FROM "public"."users"`, `"public"."users"`, true},
		{"SELECT * FROM `db`.`tbl` LIMIT 1", "`db`.`tbl`", true},
		{"SELECT * FROM (SELECT 1) x", "", false},
		{"SELECT * FROM a JOIN b", "", false},
		{"WITH x AS (SELECT 1) SELECT * FROM t", "", false},
	}
	for _, tt := range tests {
		got, ok := TableFromSimpleSelect(tt.sql)
		if ok != tt.ok || got != tt.want {
			t.Errorf("TableFromSimpleSelect(%q) = (%q, %v); want (%q, %v)", tt.sql, got, ok, tt.want, tt.ok)
		}
	}
}
