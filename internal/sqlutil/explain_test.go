package sqlutil

import "testing"

func TestWrapQueryForExplain(t *testing.T) {
	tests := []struct {
		driver   string
		in       string
		want     string
		wantOK   bool
		skipWant bool // only check ok == false
	}{
		{"postgres", "SELECT 1", "EXPLAIN SELECT 1", true, false},
		{"mysql", "SELECT 1", "EXPLAIN SELECT 1", true, false},
		{"sqlite", "SELECT 1", "EXPLAIN QUERY PLAN SELECT 1", true, false},
		{"mssql", "SELECT 1", "SET SHOWPLAN_ALL ON;\nSELECT 1\nSET SHOWPLAN_ALL OFF", true, false},
		{"postgres", "  SELECT 1  ", "EXPLAIN SELECT 1", true, false},
		{"postgres", "", "", false, true},
		{"postgres", "   \n\t  ", "", false, true},
		{"postgres", "EXPLAIN SELECT 1", "", false, true},
		{"sqlite", "EXPLAIN QUERY PLAN SELECT 1", "", false, true},
		{"mssql", "SET SHOWPLAN_ALL ON;\nSELECT 1", "", false, true},
	}
	for _, tt := range tests {
		got, ok := WrapQueryForExplain(tt.driver, tt.in)
		if ok != tt.wantOK {
			t.Errorf("WrapQueryForExplain(%q, %q) ok=%v want ok=%v", tt.driver, tt.in, ok, tt.wantOK)
			continue
		}
		if !tt.skipWant && got != tt.want {
			t.Errorf("WrapQueryForExplain(%q, %q) = %q want %q", tt.driver, tt.in, got, tt.want)
		}
	}
}
