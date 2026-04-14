package opentabs

import (
	"testing"
)

func TestParseOpenTabsData_legacyArray(t *testing.T) {
	g, by, err := parseOpenTabsData([]byte(`["a:db1","b:db2"]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Keys) != 2 || g.Active != "" || len(by) != 0 {
		t.Fatalf("got %+v by=%v", g, by)
	}
}

func TestParseOpenTabsData_legacySnapshot(t *testing.T) {
	g, by, err := parseOpenTabsData([]byte(`{"keys":["x:y"],"active":"x:y"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Keys) != 1 || g.Active != "x:y" || len(by) != 0 {
		t.Fatalf("got %+v", g)
	}
}

func TestParseOpenTabsData_v2(t *testing.T) {
	raw := `{"version":2,"global":{"keys":["g:1"],"active":"g:1"},"by_folder":{"/proj":{"keys":["p:2"],"active":"p:2"}}}`
	g, by, err := parseOpenTabsData([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Keys) != 1 || g.Keys[0] != "g:1" {
		t.Fatalf("global %+v", g)
	}
	if len(by["/proj"].Keys) != 1 || by["/proj"].Active != "p:2" {
		t.Fatalf("by_folder %+v", by)
	}
}
