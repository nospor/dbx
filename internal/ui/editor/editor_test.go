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
		line       string
		col        int
		wantStart  int
		wantEnd    int
		wantOK     bool
	}{
		{"SELECT foo", 7, 7, 10, true},   // cursor on 'f' in foo
		{"SELECT foo", 6, 7, 10, true},   // cursor on space before foo → word to right
		{"SELECT foo", 9, 7, 10, true},   // cursor on last 'o'
		{"hello world", 3, 0, 5, true},   // middle of hello
		{"hello world", 5, 6, 11, true},  // cursor on space between → word to the right (world)
		{"(bar)", 1, 1, 4, true},         // cursor after '(' → bar
		{"   x", 0, 3, 4, true},          // skip spaces to x
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
