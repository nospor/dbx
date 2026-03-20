package results

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/robertn/dbx/internal/ui/theme"
)

// QueryResult holds the outcome of a query execution.
type QueryResult struct {
	Columns []string
	Rows    [][]string
	Error   string
	Elapsed time.Duration
}

// Model is the bubbletea model for the results panel.
type Model struct {
	theme   theme.Theme
	width   int
	height  int
	focused bool

	result    *QueryResult
	cursorRow int
	cursorCol int
	scrollTop int
	scrollLeft int
	loading   bool
}

// New creates a new results model.
func New(t theme.Theme) Model {
	return Model{theme: t}
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *Model) SetFocused(f bool) {
	m.focused = f
}

func (m *Model) SetTheme(t theme.Theme) {
	m.theme = t
}

func (m *Model) SetLoading(loading bool) {
	m.loading = loading
}

func (m *Model) SetResult(r *QueryResult) {
	m.result = r
	m.cursorRow = 0
	m.cursorCol = 0
	m.scrollTop = 0
	m.scrollLeft = 0
	m.loading = false
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) {
	if m.result == nil {
		return
	}
	rows := m.result.Rows
	cols := m.result.Columns

	visibleRows := m.height - 5 // status + header + separator + hscroll bar
	if visibleRows < 1 {
		visibleRows = 1
	}

	switch msg.String() {
	case "j", "down":
		if m.cursorRow < len(rows)-1 {
			m.cursorRow++
			if m.cursorRow >= m.scrollTop+visibleRows {
				m.scrollTop++
			}
		}
	case "k", "up":
		if m.cursorRow > 0 {
			m.cursorRow--
			if m.cursorRow < m.scrollTop {
				m.scrollTop--
			}
		}
	case "h", "left":
		if m.cursorCol > 0 {
			m.cursorCol--
			if m.cursorCol < m.scrollLeft {
				m.scrollLeft = m.cursorCol
			}
		}
	case "l", "right":
		if m.cursorCol < len(cols)-1 {
			m.cursorCol++
			m.adjustScrollLeft()
		}
	case "g":
		m.cursorRow = 0
		m.scrollTop = 0
	case "G":
		m.cursorRow = len(rows) - 1
		if m.cursorRow >= visibleRows {
			m.scrollTop = m.cursorRow - visibleRows + 1
		}
	case "0":
		m.cursorCol = 0
		m.scrollLeft = 0
	case "$":
		m.cursorCol = len(cols) - 1
		m.adjustScrollLeft()
	}
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	if m.loading {
		return lipgloss.NewStyle().Width(m.width).Height(m.height).
			Render(m.theme.Dimmed.Render("Running query..."))
	}

	if m.result == nil {
		return lipgloss.NewStyle().Width(m.width).Height(m.height).
			Render(m.theme.Dimmed.Render("No results yet.\nPress ctrl+enter in the editor to execute a query."))
	}

	if m.result.Error != "" {
		errMsg := m.theme.Error.Render("Error: " + m.result.Error)
		return lipgloss.NewStyle().Width(m.width).Height(m.height).Render(errMsg)
	}

	var sb strings.Builder

	// Status bar
	elapsed := ""
	if m.result.Elapsed > 0 {
		elapsed = fmt.Sprintf(" | %s", m.result.Elapsed.Round(time.Millisecond))
	}
	colInfo := ""
	if len(m.result.Columns) > 0 {
		colInfo = fmt.Sprintf(" | col %d/%d", m.cursorCol+1, len(m.result.Columns))
	}
	status := fmt.Sprintf("%d rows | row %d%s%s",
		len(m.result.Rows), m.cursorRow+1, colInfo, elapsed)
	sb.WriteString(m.theme.StatusBar.Width(m.width).Render(status) + "\n")

	if len(m.result.Columns) == 0 {
		sb.WriteString(m.theme.Dimmed.Render("Query executed successfully (no rows returned)."))
		return lipgloss.NewStyle().Width(m.width).Height(m.height).Render(sb.String())
	}

	colWidths := m.computeColWidths()
	availWidth := m.width

	// Build the list of visible column indices starting from scrollLeft
	visibleCols := []int{}
	used := 0
	for ci := m.scrollLeft; ci < len(colWidths); ci++ {
		needed := colWidths[ci]
		if ci > m.scrollLeft {
			needed += 3 // " │ "
		}
		if used+needed > availWidth {
			break
		}
		used += needed
		visibleCols = append(visibleCols, ci)
	}

	renderRow := func(cells []string, rowIdx int, isHeader bool) string {
		var parts []string
		for _, ci := range visibleCols {
			var cell string
			if isHeader {
				cell = m.result.Columns[ci]
			} else if ci < len(cells) {
				cell = cells[ci]
			}
			cell = truncate(cell, colWidths[ci])
			cell = padRight(cell, colWidths[ci])

			var style lipgloss.Style
			switch {
			case isHeader:
				style = m.theme.TableHeader
			case ci == m.cursorCol && rowIdx == m.cursorRow && m.focused:
				style = m.theme.TableCursorCell
			case rowIdx == m.cursorRow && m.focused:
				style = m.theme.TableCursorRow
			case rowIdx%2 == 0:
				style = m.theme.TableRow
			default:
				style = m.theme.TableRowAlt
			}
			parts = append(parts, style.Render(cell))
		}
		return strings.Join(parts, m.theme.Dimmed.Render(" │ "))
	}

	// Header
	sb.WriteString(renderRow(nil, -1, true) + "\n")

	// Separator
	var sepParts []string
	for _, ci := range visibleCols {
		sepParts = append(sepParts, strings.Repeat("─", colWidths[ci]))
	}
	sb.WriteString(m.theme.Dimmed.Render(strings.Join(sepParts, "─┼─")) + "\n")

	// Data rows
	visibleRows := m.height - 5
	if visibleRows < 1 {
		visibleRows = 1
	}
	for i := m.scrollTop; i < m.scrollTop+visibleRows && i < len(m.result.Rows); i++ {
		sb.WriteString(renderRow(m.result.Rows[i], i, false) + "\n")
	}

	// Horizontal scroll indicator
	sb.WriteString(m.renderHScrollBar(len(colWidths)) + "\n")

	return lipgloss.NewStyle().Width(m.width).Height(m.height).Render(sb.String())
}

// renderHScrollBar draws a one-line scroll indicator showing horizontal position.
func (m Model) renderHScrollBar(totalCols int) string {
	if totalCols == 0 || m.width < 4 {
		return ""
	}

	// Count how many columns are currently visible
	colWidths := m.computeColWidths()
	visibleColCount := 0
	used := 0
	for ci := m.scrollLeft; ci < len(colWidths); ci++ {
		needed := colWidths[ci]
		if ci > m.scrollLeft {
			needed += 3
		}
		if used+needed > m.width {
			break
		}
		used += needed
		visibleColCount++
	}
	if visibleColCount < 1 {
		visibleColCount = 1
	}

	// If everything fits, no scrollbar needed
	if visibleColCount >= totalCols {
		return m.theme.Dimmed.Render("h/l: scroll cols  0/$: first/last col")
	}

	barWidth := m.width - 2
	if barWidth < 2 {
		barWidth = 2
	}

	// Thumb width proportional to visible/total ratio
	thumbW := barWidth * visibleColCount / totalCols
	if thumbW < 1 {
		thumbW = 1
	}
	if thumbW > barWidth {
		thumbW = barWidth
	}

	// Thumb position: scrollLeft out of (totalCols - visibleColCount) possible positions
	maxScroll := totalCols - visibleColCount
	thumbPos := 0
	if maxScroll > 0 {
		thumbPos = (barWidth - thumbW) * m.scrollLeft / maxScroll
	}

	track := []rune(strings.Repeat("─", barWidth))
	for i := 0; i < thumbW; i++ {
		if thumbPos+i < len(track) {
			track[thumbPos+i] = '█'
		}
	}

	left := m.theme.Dimmed.Render("◀")
	right := m.theme.Dimmed.Render("▶")
	bar := m.theme.Dimmed.Render(string(track))
	hint := m.theme.Dimmed.Render(fmt.Sprintf(" col %d/%d  h/l: scroll  0/$: first/last", m.scrollLeft+1, totalCols))

	return left + bar + right + hint
}

// Result returns the current query result, or nil.
func (m Model) Result() *QueryResult {
	return m.result
}

// SelectedCell returns the value of the currently selected cell.
func (m Model) SelectedCell() string {
	if m.result == nil || m.cursorRow >= len(m.result.Rows) {
		return ""
	}
	row := m.result.Rows[m.cursorRow]
	if m.cursorCol >= len(row) {
		return ""
	}
	return row[m.cursorCol]
}

// SelectedRow returns all cell values for the currently selected row.
func (m Model) SelectedRow() []string {
	if m.result == nil || m.cursorRow >= len(m.result.Rows) {
		return nil
	}
	return m.result.Rows[m.cursorRow]
}

// adjustScrollLeft advances scrollLeft until cursorCol is within the visible window.
func (m *Model) adjustScrollLeft() {
	if m.result == nil {
		return
	}
	colWidths := m.computeColWidths()
	availWidth := m.width

	// Ensure scrollLeft never exceeds cursorCol
	if m.scrollLeft > m.cursorCol {
		m.scrollLeft = m.cursorCol
	}
	for {
		used := 0
		visible := false
		for ci := m.scrollLeft; ci < len(colWidths); ci++ {
			needed := colWidths[ci]
			if ci > m.scrollLeft {
				needed += 3
			}
			if used+needed > availWidth {
				break
			}
			used += needed
			if ci == m.cursorCol {
				visible = true
				break
			}
		}
		if visible || m.scrollLeft >= m.cursorCol {
			break
		}
		m.scrollLeft++
	}
}

// computeColWidths returns capped column widths based on header and data.
func (m *Model) computeColWidths() []int {
	if m.result == nil {
		return nil
	}
	colWidths := make([]int, len(m.result.Columns))
	for i, col := range m.result.Columns {
		colWidths[i] = len([]rune(col))
	}
	for _, row := range m.result.Rows {
		for i, cell := range row {
			if i < len(colWidths) {
				if w := len([]rune(cell)); w > colWidths[i] {
					colWidths[i] = w
				}
			}
		}
	}
	for i := range colWidths {
		if colWidths[i] > 40 {
			colWidths[i] = 40
		}
		if colWidths[i] < 1 {
			colWidths[i] = 1
		}
	}
	return colWidths
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 3 {
		return string(runes[:n])
	}
	return string(runes[:n-3]) + "..."
}
