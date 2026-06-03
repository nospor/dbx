package editor

import (
	"strings"
	"testing"

	"github.com/robertn/dbx/internal/ui/theme"
)

func TestRenderTabBar_ScrollingAndCentering(t *testing.T) {
	t.Parallel()

	th := theme.Terminal()
	openTabs := []TabInfo{
		{ID: "t0", ConnKey: "t0", Label: "Tab0"},
		{ID: "t1", ConnKey: "t1", Label: "Tab1"},
		{ID: "t2", ConnKey: "t2", Label: "Tab2"},
		{ID: "t3", ConnKey: "t3", Label: "Tab3"},
		{ID: "t4", ConnKey: "t4", Label: "Tab4"},
	}

	t.Run("all fit", func(t *testing.T) {
		// totalWidth = 100, plenty of space
		got := renderTabBar(th, openTabs, 2, " NORMAL ", 100)
		// Should contain all tabs and mode label
		for _, tab := range openTabs {
			if !strings.Contains(got, tab.Label) {
				t.Errorf("expected tab %q to be visible, got: %q", tab.Label, got)
			}
		}
		if strings.Contains(got, "…") {
			t.Errorf("expected no truncation/scrolling indicators, got: %q", got)
		}
	})

	t.Run("centered scroll left and right", func(t *testing.T) {
		// totalWidth is constrained so not all tabs fit.
		// Tab label widths rendered:
		// " Tab0 " (6), " Tab1 " (6), " Tab2 " (6), " Tab3 " (6), " Tab4 " (6)
		// Plus Padding(0, 1) on active/inactive tabs = 8 cells per tab.
		// Mode label: " NORMAL " (6 + 2 padding = 8)
		// Budget: totalWidth - modeWidth - 1 = 31 - 8 - 1 = 22.
		// Let's see what fits:
		// "… Tab1 Tab2 …" -> 2 (indicator) + 8 (Tab1) + 1 (space) + 8 (Tab2) + 2 (indicator) = 21. Fits!
		// "… Tab1 Tab2 Tab3 …" -> 2 + 8 + 1 + 8 + 1 + 8 + 2 = 30. Exceeds 22.
		got := renderTabBar(th, openTabs, 2, " NORMAL ", 31)

		// Under budget 22:
		// Let's verify that Tab2 (active) is present.
		if !strings.Contains(got, "Tab2") {
			t.Errorf("expected active tab Tab2 to be present, got: %q", got)
		}
		// Since it scrolls/truncates on both ends, ellipsis indicators should be present.
		if !strings.Contains(got, "…") {
			t.Errorf("expected ellipsis indicator, got: %q", got)
		}
	})

	t.Run("no left ellipsis when first tab is visible", func(t *testing.T) {
		// Active tab is Tab0 (index 0).
		// We have constrained budget, so it will scroll on the right but not left.
		// totalWidth = 31 -> Budget = 22.
		// Tab0, Tab1 (width: 8 + 1 + 8 + 2 (suffix) = 19, fits 22)
		// Tab0, Tab1, Tab2 (width: 8 + 1 + 8 + 1 + 8 + 2 (suffix) = 28, exceeds 22)
		got := renderTabBar(th, openTabs, 0, " NORMAL ", 31)
		if !strings.Contains(got, "Tab0") {
			t.Errorf("expected Tab0, got: %q", got)
		}
		if !strings.Contains(got, "Tab1") {
			t.Errorf("expected Tab1, got: %q", got)
		}
		if strings.Contains(got, "Tab2") {
			t.Errorf("did not expect Tab2, got: %q", got)
		}
		// Left ellipsis should NOT be present because Tab0 is visible.
		// Right ellipsis should be present.
		count := strings.Count(got, "…")
		if count != 1 {
			t.Errorf("expected exactly one ellipsis (on the right), got %d: %q", count, got)
		}
	})

	t.Run("no right ellipsis when last tab is visible", func(t *testing.T) {
		// Active tab is Tab4 (index 4).
		// totalWidth = 31 -> Budget = 22.
		// Tab3, Tab4 (width: 2 (prefix) + 8 + 1 + 8 = 19, fits 22)
		// Tab2, Tab3, Tab4 (width: 2 (prefix) + 8 + 1 + 8 + 1 + 8 = 28, exceeds 22)
		got := renderTabBar(th, openTabs, 4, " NORMAL ", 31)
		if !strings.Contains(got, "Tab4") {
			t.Errorf("expected Tab4, got: %q", got)
		}
		if !strings.Contains(got, "Tab3") {
			t.Errorf("expected Tab3, got: %q", got)
		}
		if strings.Contains(got, "Tab2") {
			t.Errorf("did not expect Tab2, got: %q", got)
		}
		// Right ellipsis should NOT be present. Left ellipsis should be present.
		count := strings.Count(got, "…")
		if count != 1 {
			t.Errorf("expected exactly one ellipsis (on the left), got %d: %q", count, got)
		}
	})
}
