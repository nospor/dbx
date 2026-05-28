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

	var parts []string
	for i, tab := range openTabs {
		label := tab.Label
		if label == "" {
			label = tab.ConnKey
		}
		label = " " + strings.TrimSpace(label) + " "
		if i == activeIdx {
			parts = append(parts, t.TabActive.Render(label))
		} else {
			parts = append(parts, t.TabInactive.Render(label))
		}
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
