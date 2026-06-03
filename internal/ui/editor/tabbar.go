package editor

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/robertn/dbx/internal/ui/theme"
)

// TabInfo is one open editor tab (connection + database).
type TabInfo struct {
	ID      string
	ConnKey string
	Label   string
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func renderTabBar(t theme.Theme, openTabs []TabInfo, activeIdx int, modeLabel string, totalWidth int) string {
	if totalWidth < 1 {
		return ""
	}
	modeBlock := t.StatusBarMode.Render(strings.TrimSpace(modeLabel))
	modeW := lipgloss.Width(modeBlock)
	if modeW >= totalWidth {
		return lipgloss.NewStyle().Width(totalWidth).Render(ansi.Truncate(modeBlock, totalWidth, "…"))
	}
	tabBudget := totalWidth - modeW - 1
	if tabBudget < 4 {
		tabBudget = maxInt(1, totalWidth-modeW)
	}

	if len(openTabs) == 0 {
		left := t.Dimmed.Render(" — no tab — ")
		pad := totalWidth - lipgloss.Width(left) - lipgloss.Width(modeBlock)
		if pad < 1 {
			pad = 1
		}
		return left + strings.Repeat(" ", pad) + modeBlock
	}

	if activeIdx < 0 || activeIdx >= len(openTabs) {
		activeIdx = 0
	}

	n := len(openTabs)
	rendered := make([]string, n)
	widths := make([]int, n)
	for i, tab := range openTabs {
		label := tab.Label
		if label == "" {
			label = tab.ConnKey
		}
		label = " " + strings.TrimSpace(label) + " "
		if i == activeIdx {
			rendered[i] = t.TabActive.Render(label)
		} else {
			rendered[i] = t.TabInactive.Render(label)
		}
		widths[i] = lipgloss.Width(rendered[i])
	}

	rangeWidth := func(start, end int) int {
		if start > end {
			return 0
		}
		w := 0
		for i := start; i <= end; i++ {
			w += widths[i]
		}
		w += (end - start) // spaces between tabs
		if start > 0 {
			w += 2 // prefix "… "
		}
		if end < n-1 {
			w += 2 // suffix " …"
		}
		return w
	}

	start := activeIdx
	end := activeIdx

	if rangeWidth(start, end) <= tabBudget {
		for start > 0 || end < n-1 {
			leftWidth := 0
			for i := start; i < activeIdx; i++ {
				leftWidth += widths[i] + 1
			}
			rightWidth := 0
			for i := activeIdx + 1; i <= end; i++ {
				rightWidth += 1 + widths[i]
			}

			var tryLeft, tryRight bool
			if start > 0 && end < n-1 {
				if leftWidth <= rightWidth {
					tryLeft = true
				} else {
					tryRight = true
				}
			} else if start > 0 {
				tryLeft = true
			} else if end < n-1 {
				tryRight = true
			} else {
				break
			}

			expanded := false
			if tryLeft {
				if rangeWidth(start-1, end) <= tabBudget {
					start--
					expanded = true
				} else if end < n-1 && rangeWidth(start, end+1) <= tabBudget {
					end++
					expanded = true
				}
			} else if tryRight {
				if rangeWidth(start, end+1) <= tabBudget {
					end++
					expanded = true
				} else if start > 0 && rangeWidth(start-1, end) <= tabBudget {
					start--
					expanded = true
				}
			}

			if !expanded {
				break
			}
		}
	}

	var parts []string
	if start > 0 {
		parts = append(parts, t.Dimmed.Render("…"))
	}
	for i := start; i <= end; i++ {
		parts = append(parts, rendered[i])
	}
	if end < n-1 {
		parts = append(parts, t.Dimmed.Render("…"))
	}
	joined := strings.Join(parts, " ")

	if lipgloss.Width(joined) > tabBudget {
		joined = ansi.Truncate(joined, tabBudget, "…")
	}
	pad := totalWidth - lipgloss.Width(joined) - lipgloss.Width(modeBlock)
	if pad < 1 {
		pad = 1
	}
	return joined + strings.Repeat(" ", pad) + modeBlock
}
