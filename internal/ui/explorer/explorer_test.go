package explorer

import "testing"

// filterFixture is:
//
//	conn
//	  db0
//	    pad, pad, ...
//	  db1
//	    alpha_hit, tbl, tbl, beta_hit, tbl, ..., last
//	  db2
//	    other_hit
func filterFixture(lastLabel string) (Model, *Node, []*Node) {
	conn := &Node{Kind: NodeConnection, Label: "c", ConnID: "c1", Expanded: true}
	db0 := &Node{Kind: NodeDatabase, Label: "db0", ConnID: "c1", DBName: "db0", Expanded: true, parent: conn}
	db1 := &Node{Kind: NodeDatabase, Label: "db1", ConnID: "c1", DBName: "db1", Expanded: true, parent: conn}
	db2 := &Node{Kind: NodeDatabase, Label: "db2", ConnID: "c1", DBName: "db2", Expanded: true, parent: conn}
	conn.Children = []*Node{db0, db1, db2}

	for i := 0; i < 10; i++ {
		db0.Children = append(db0.Children, &Node{
			Kind: NodeTable, Label: "pad", ConnID: "c1", DBName: "db0", parent: db0,
		})
	}

	db1Tables := make([]*Node, 20)
	for i := range db1Tables {
		db1Tables[i] = &Node{
			Kind: NodeTable, Label: "tbl", ConnID: "c1", DBName: "db1", parent: db1,
		}
	}
	db1Tables[0].Label = "alpha_hit"
	db1Tables[3].Label = "beta_hit"
	db1Tables[19].Label = lastLabel
	db1.Children = db1Tables

	db2.Children = []*Node{{
		Kind: NodeTable, Label: "other_hit", ConnID: "c1", DBName: "db2", parent: db2,
	}}

	m := Model{nodes: []*Node{conn}}
	m.flatten()
	return m, db1, db1Tables
}

func flatIndex(t *testing.T, m Model, n *Node) int {
	t.Helper()
	for i, fn := range m.flat {
		if fn == n {
			return i
		}
	}
	t.Fatalf("node %q not in flat list", n.Label)
	return -1
}

func TestApplyFilterTextKeepsCursorInCurrentDatabase(t *testing.T) {
	t.Parallel()

	t.Run("last row clamps to last match in the same database", func(t *testing.T) {
		m, _, tables := filterFixture("tbl")
		m.cursor = flatIndex(t, m, tables[19])
		m.applyFilterText("hit")
		got := m.flat[m.cursor]
		if got != tables[3] {
			t.Fatalf("cursor = %q (%s), want beta_hit in db1", got.Label, got.DBName)
		}
	})

	t.Run("matching row stays selected when rows above disappear", func(t *testing.T) {
		m, _, tables := filterFixture("zzz_hit")
		m.cursor = flatIndex(t, m, tables[19])
		m.applyFilterText("hit")
		got := m.flat[m.cursor]
		if got != tables[19] {
			t.Fatalf("cursor = %q (%s), want zzz_hit in db1", got.Label, got.DBName)
		}
	})

	t.Run("no match in the current database lands on that database", func(t *testing.T) {
		m, db1, tables := filterFixture("tbl")
		m.cursor = flatIndex(t, m, tables[19])
		m.applyFilterText("nomatch")
		got := m.flat[m.cursor]
		if got != db1 {
			t.Fatalf("cursor = %q (%s), want db1", got.Label, got.DBName)
		}
	})

	t.Run("database row stays put when earlier lists shrink", func(t *testing.T) {
		m, db1, _ := filterFixture("tbl")
		m.cursor = flatIndex(t, m, db1)
		m.applyFilterText("hit")
		got := m.flat[m.cursor]
		if got != db1 {
			t.Fatalf("cursor = %q (%s), want db1", got.Label, got.DBName)
		}
	})
}

func TestFilterTablesOnlyShowsAllColumnsWhenExpanded(t *testing.T) {
	t.Parallel()

	conn := &Node{Kind: NodeConnection, Label: "c", ConnID: "c1", Expanded: true}
	db := &Node{Kind: NodeDatabase, Label: "db", ConnID: "c1", DBName: "db", Expanded: true, parent: conn}
	conn.Children = []*Node{db}

	tbl := &Node{
		Kind: NodeTable, Label: "users", ConnID: "c1", DBName: "db", Expanded: true, parent: db,
	}
	tbl.Children = []*Node{
		{Kind: NodeColumn, Label: "id", parent: tbl},
		{Kind: NodeColumn, Label: "email", parent: tbl},
		{Kind: NodeColumn, Label: "created_at", parent: tbl},
	}
	db.Children = []*Node{tbl}

	m := Model{nodes: []*Node{conn}}
	m.applyFilterText("users")

	var cols int
	for _, n := range m.flat {
		if n.Kind == NodeColumn {
			cols++
		}
	}
	if cols != 3 {
		t.Fatalf("got %d columns in flat list, want 3 (filter must not hide columns)", cols)
	}
}
