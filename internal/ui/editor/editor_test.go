package editor

import "testing"

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
