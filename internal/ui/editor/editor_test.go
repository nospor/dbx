package editor

import (
	"strings"
	"testing"

	"github.com/robertn/dbx/internal/ui/theme"
)

func TestCompletionPopupStartRow(t *testing.T) {
	t.Parallel()
	// totalRows mimics strings.Split(editor View, "\n") including a trailing "" after final newline.
	const totalRows = 13

	t.Run("fits below with margin", func(t *testing.T) {
		t.Parallel()
		// last usable row index = 10; below must end by 10. cursor row 2 → belowStart 3; boxH 8 → end 10.
		if got := completionPopupStartRow(2, 8, totalRows); got != 3 {
			t.Fatalf("got %d want 3", got)
		}
	})

	t.Run("flip above when below overflows", func(t *testing.T) {
		t.Parallel()
		// last=10; below from 12 overflows; above: start 11-10=1, ends at row 10.
		if got := completionPopupStartRow(11, 10, totalRows); got != 1 {
			t.Fatalf("got %d want 1", got)
		}
	})

	t.Run("pin bottom when neither below nor above fits", func(t *testing.T) {
		t.Parallel()
		if got := completionPopupStartRow(5, 10, 8); got != 1 {
			t.Fatalf("got %d want 1", got)
		}
	})
}

func TestWrappedRowsForVisual_empty(t *testing.T) {
	t.Parallel()
	got := wrappedRowsForVisual("", 10)
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("got %#v", got)
	}
}

func TestTotalWrappedDisplayRows_longLogicalLine(t *testing.T) {
	t.Parallel()
	m := New(theme.Dark())
	m.SwitchConnection("c")
	m.setLines([]string{strings.Repeat("x", 100)})
	const w = 20
	n := m.totalWrappedDisplayRows(m.lines(), w)
	if n < 5 {
		t.Fatalf("expected at least 5 wrapped rows for 100 chars at width 20, got %d", n)
	}
}

func TestGlobalCursorDisplayRow_secondLogicalLine(t *testing.T) {
	t.Parallel()
	m := New(theme.Dark())
	m.SwitchConnection("c")
	m.setLines([]string{"short", strings.Repeat("y", 80)})
	m.SetFocused(true)
	m.vim.row = 1
	m.vim.col = 0
	const w = 25
	first := m.wrappedRowCount(m.lines(), 0, w)
	if got := m.globalCursorDisplayRow(m.lines(), w); got != first {
		t.Fatalf("globalCursorDisplayRow got %d want %d (first line wrapped height)", got, first)
	}
}

func TestCurrentWordBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line      string
		col       int
		wantStart int
		wantEnd   int
		wantOK    bool
	}{
		{"SELECT foo", 7, 7, 10, true},  // cursor on 'f' in foo
		{"SELECT foo", 6, 7, 10, true},  // cursor on space before foo → word to right
		{"SELECT foo", 9, 7, 10, true},  // cursor on last 'o'
		{"hello world", 3, 0, 5, true},  // middle of hello
		{"hello world", 5, 6, 11, true}, // cursor on space between → word to the right (world)
		{"(bar)", 1, 1, 4, true},        // cursor after '(' → bar
		{"   x", 0, 3, 4, true},         // skip spaces to x
		{"", 0, 0, 0, false},
		{"   ", 1, 0, 0, false},
	}
	for _, tc := range tests {
		gotS, gotE, gotOK := currentWordBounds(tc.line, tc.col)
		if gotOK != tc.wantOK || gotS != tc.wantStart || gotE != tc.wantEnd {
			t.Errorf("currentWordBounds(%q, %d) = (%d,%d,%v) want (%d,%d,%v)",
				tc.line, tc.col, gotS, gotE, gotOK, tc.wantStart, tc.wantEnd, tc.wantOK)
		}
	}
}

func TestCleanLineAndQuery(t *testing.T) {
	t.Parallel()

	t.Run("cleanLineText patterns", func(t *testing.T) {
		tests := []struct {
			input string
			want  string
		}{
			{"     1 │SELECT smid_id, smid_unit_id, prod_id", "SELECT smid_id, smid_unit_id, prod_id"},
			{"     4 │   OR (smid_id = 617955 AND smid_unit_id = 0);", "  OR (smid_id = 617955 AND smid_unit_id = 0);"},
			{"  1 | SELECT", "SELECT"},
			{"1│SELECT", "SELECT"},
			{"SELECT 1", "SELECT 1"},
			{"123", "123"},
			{"  123  ", "  123  "},
			{"     1 │\tSELECT", "SELECT"},
			{"     1 │ \tSELECT", "\tSELECT"},
		}

		for _, tc := range tests {
			got := cleanLineText(tc.input)
			if got != tc.want {
				t.Errorf("cleanLineText(%q) = %q; want %q", tc.input, got, tc.want)
			}
		}
	})

	t.Run("cleanLine in model", func(t *testing.T) {
		m := New(theme.Dark())
		m.SwitchConnection("c")
		m.setLines([]string{
			"     1 │SELECT a",
			"     2 │  FROM b",
		})
		m.vim.row = 1
		lines := m.cleanLine(m.lines())
		if len(lines) != 2 || lines[1] != " FROM b" {
			t.Errorf("expected ' FROM b', got %#v", lines)
		}
	})

	t.Run("cleanQuery in model", func(t *testing.T) {
		m := New(theme.Dark())
		m.SwitchConnection("c")
		m.setLines([]string{
			"     1 │SELECT a",
			"     2 │  FROM b",
			"",
			"     3 │WHERE c",
		})
		// Cursor on first query block
		m.vim.row = 0
		lines := m.cleanQuery(m.lines())
		if lines[0] != "SELECT a" || lines[1] != " FROM b" || lines[2] != "" || lines[3] != "     3 │WHERE c" {
			t.Errorf("unexpected cleaned lines: %#v", lines)
		}
	})
}

func TestMultiTabs(t *testing.T) {
	t.Parallel()
	m := New(theme.Dark())
	
	// Helper functions tests
	if got := cleanTabID("conn#1"); got != "conn" {
		t.Errorf("expected cleanTabID to be 'conn', got %q", got)
	}
	if got := cleanTabID("conn"); got != "conn" {
		t.Errorf("expected cleanTabID to be 'conn', got %q", got)
	}
	if got := tabLabelSuffix("conn#1"); got != " (2)" {
		t.Errorf("expected suffix to be ' (2)', got %q", got)
	}
	if got := tabLabelSuffix("conn"); got != "" {
		t.Errorf("expected suffix to be empty, got %q", got)
	}

	// Tab opening tests
	m.OpenTab("conn1", "Conn 1")
	if len(m.openTabs) != 1 || m.openTabs[0].ID != "conn1" || m.openTabs[0].Label != "Conn 1" {
		t.Errorf("expected one tab 'conn1', got %#v", m.openTabs)
	}

	// Open another tab for the same connection key using OpenNewTab
	switched := m.OpenNewTab()
	if switched == nil {
		t.Fatal("expected OpenNewTab to return non-nil switched msg")
	}
	if len(m.openTabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(m.openTabs))
	}
	if m.openTabs[1].ID != "conn1#1" || m.openTabs[1].Label != "Conn 1 (2)" {
		t.Errorf("expected second tab ID 'conn1#1' and Label 'Conn 1 (2)', got ID %q, Label %q", m.openTabs[1].ID, m.openTabs[1].Label)
	}
	if m.activeTabIdx != 1 || m.activeTabID != "conn1#1" {
		t.Errorf("expected active tab index 1, ID 'conn1#1', got index %d, ID %q", m.activeTabIdx, m.activeTabID)
	}

	// SwitchedMsg should carry the active tab ID
	if switched.ConnKey != "conn1#1" {
		t.Errorf("expected switched.ConnKey to be 'conn1#1', got %q", switched.ConnKey)
	}

	// OpenTab on same connection key should stay on current active tab (conn1#1) because it has the same ConnKey
	m.OpenTab("conn1", "Conn 1")
	if m.activeTabIdx != 1 || m.activeTabID != "conn1#1" {
		t.Errorf("expected to stay on active tab index 1, got index %d, ID %q", m.activeTabIdx, m.activeTabID)
	}

	// Verify insertion position.
	// Currently m.openTabs is: [conn1, conn1#1]
	// Let's activate the first tab (index 0)
	m.activateTabIndex(0)
	// Now call OpenNewTab. It should insert the new tab at index 1.
	// The list should become: [conn1, conn1#2, conn1#1]
	switched2 := m.OpenNewTab()
	if switched2 == nil {
		t.Fatal("expected OpenNewTab to return non-nil switched msg")
	}
	if len(m.openTabs) != 3 {
		t.Fatalf("expected 3 tabs, got %d", len(m.openTabs))
	}
	if m.openTabs[0].ID != "conn1" || m.openTabs[1].ID != "conn1#2" || m.openTabs[2].ID != "conn1#1" {
		t.Errorf("unexpected tab order: got ID[0]=%q, ID[1]=%q, ID[2]=%q", m.openTabs[0].ID, m.openTabs[1].ID, m.openTabs[2].ID)
	}
	if m.activeTabIdx != 1 || m.activeTabID != "conn1#2" {
		t.Errorf("expected active tab index 1, ID 'conn1#2', got index %d, ID %q", m.activeTabIdx, m.activeTabID)
	}
}

func TestCursorStatePreservedOnTabSwitch(t *testing.T) {
	t.Parallel()
	m := New(theme.Dark())

	// Open two tabs
	m.OpenTab("conn1", "Conn 1")
	m.setLines([]string{"SELECT 1;", "SELECT 2;", "SELECT 3;"})
	m.vim.row = 2
	m.vim.col = 5
	m.scrollTop = 1

	m.OpenTab("conn2", "Conn 2")
	m.setLines([]string{"SELECT A;", "SELECT B;"})
	m.vim.row = 1
	m.vim.col = 3
	m.scrollTop = 0

	// Switch back to conn1
	m.activateTabIndex(0)
	if m.vim.row != 2 || m.vim.col != 5 || m.scrollTop != 1 {
		t.Errorf("expected conn1 state to be preserved (row=2, col=5, scrollTop=1), got row=%d, col=%d, scrollTop=%d", m.vim.row, m.vim.col, m.scrollTop)
	}

	// Switch back to conn2
	m.activateTabIndex(1)
	if m.vim.row != 1 || m.vim.col != 3 || m.scrollTop != 0 {
		t.Errorf("expected conn2 state to be preserved (row=1, col=3, scrollTop=0), got row=%d, col=%d, scrollTop=%d", m.vim.row, m.vim.col, m.scrollTop)
	}
}
