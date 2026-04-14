package querycontents

import "testing"

func TestParseQueryContentsData_legacyFlat(t *testing.T) {
	g, by, err := parseQueryContentsData([]byte(`{"a:b":"SELECT 1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if g["a:b"] != "SELECT 1" || len(by) != 0 {
		t.Fatalf("g=%v by=%v", g, by)
	}
}

func TestParseQueryContentsData_v2(t *testing.T) {
	raw := `{"version":2,"global":{"g:1":"x"},"by_folder":{"/p":{"p:2":"y"}}}`
	g, by, err := parseQueryContentsData([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if g["g:1"] != "x" || by["/p"]["p:2"] != "y" {
		t.Fatalf("g=%v by=%v", g, by)
	}
}
