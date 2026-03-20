package results

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/robertn/dbx/internal/sqlutil"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/robertn/dbx/internal/ui/theme"
	"github.com/robertn/dbx/internal/util"
)

// QueryResult holds the outcome of a query execution.
type QueryResult struct {
	Columns   []string
	Rows      [][]string
	Error     string
	Elapsed   time.Duration
	SourceSQL string // original SELECT (for delete-row draft generation)
	Driver    string // connection driver name (for SQL quoting)
	Database  string // active database name when the query ran
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

	showCellPopup bool
	cellPopupTop  int
	cellPopupMsg  string

	// Row marks for bulk delete draft (s / S); rangeSelect + rangeAnchor for Shift+S band selection
	selectedRows map[int]struct{}
	rangeSelect  bool
	rangeAnchor  int

	// Update-cell input popup
	showUpdatePopup bool
	updateInput     []rune
	updateCursorPos int
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
	m.showCellPopup = false
	m.cellPopupTop = 0
	m.cellPopupMsg = ""
	m.selectedRows = nil
	m.rangeSelect = false
	m.rangeAnchor = 0
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
		cmd := m.handleKey(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	if m.result == nil {
		return nil
	}
	if m.showUpdatePopup {
		return m.handleUpdatePopupKey(msg)
	}
	if m.showCellPopup {
		m.handleCellPopupKey(msg)
		return nil
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
		m.syncRangeSelection()
	case "k", "up":
		if m.cursorRow > 0 {
			m.cursorRow--
			if m.cursorRow < m.scrollTop {
				m.scrollTop--
			}
		}
		m.syncRangeSelection()
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
		m.syncRangeSelection()
	case "G":
		m.cursorRow = len(rows) - 1
		if m.cursorRow >= visibleRows {
			m.scrollTop = m.cursorRow - visibleRows + 1
		}
		m.syncRangeSelection()
	case "0":
		m.cursorCol = 0
		m.scrollLeft = 0
	case "$":
		m.cursorCol = len(cols) - 1
		m.adjustScrollLeft()
	case "v":
		m.showCellPopup = true
		m.cellPopupTop = 0
		m.cellPopupMsg = ""
	case "esc":
		m.rangeSelect = false
		m.selectedRows = nil
	case "s":
		m.rangeSelect = false
		if m.selectedRows == nil {
			m.selectedRows = make(map[int]struct{})
		}
		if _, ok := m.selectedRows[m.cursorRow]; ok {
			delete(m.selectedRows, m.cursorRow)
		} else {
			m.selectedRows[m.cursorRow] = struct{}{}
		}
	case "S":
		m.rangeSelect = true
		m.rangeAnchor = m.cursorRow
		m.syncRangeSelection()
	case "d":
		return m.deleteDraftCmd()
	case "u":
		m.openUpdatePopup()
	}
	return nil
}

func (m *Model) handleCellPopupKey(msg tea.KeyMsg) {
	lines := m.selectedCellLines()
	maxTop := 0
	visible := m.height - 8
	if visible < 1 {
		visible = 1
	}
	if len(lines) > visible {
		maxTop = len(lines) - visible
	}

	switch msg.String() {
	case "esc", "enter", "q", "v":
		m.showCellPopup = false
		m.cellPopupMsg = ""
	case "y":
		if err := util.Copy(m.SelectedCell()); err != nil {
			m.cellPopupMsg = "Clipboard unavailable: " + err.Error()
		}
	case "j", "down":
		if m.cellPopupTop < maxTop {
			m.cellPopupTop++
		}
	case "k", "up":
		if m.cellPopupTop > 0 {
			m.cellPopupTop--
		}
	case "pgdown", "ctrl+f":
		m.cellPopupTop += visible
		if m.cellPopupTop > maxTop {
			m.cellPopupTop = maxTop
		}
	case "pgup", "ctrl+b":
		m.cellPopupTop -= visible
		if m.cellPopupTop < 0 {
			m.cellPopupTop = 0
		}
	case "g":
		m.cellPopupTop = 0
	case "G":
		m.cellPopupTop = maxTop
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
			Render(m.theme.Dimmed.Render("No results yet.\nPress enter in the editor to execute a query."))
	}

	if m.result.Error != "" {
		errMsg := m.theme.Error.Render("Error: " + m.result.Error)
		return lipgloss.NewStyle().Width(m.width).Height(m.height).Render(errMsg)
	}
	if m.showUpdatePopup {
		return lipgloss.NewStyle().Width(m.width).Height(m.height).Render(m.renderUpdatePopup())
	}
	if m.showCellPopup {
		return lipgloss.NewStyle().Width(m.width).Height(m.height).Render(m.renderCellPopup())
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
	markInfo := ""
	if m.selectedRows != nil && len(m.selectedRows) > 0 {
		markInfo = fmt.Sprintf(" | %d marked", len(m.selectedRows))
	}
	status := fmt.Sprintf("%d rows | row %d%s%s%s",
		len(m.result.Rows), m.cursorRow+1, colInfo, markInfo, elapsed)
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
				cell = sanitizeTableCell(cells[ci])
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
			case m.isRowSelected(rowIdx):
				style = m.theme.TableRowSelected
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
		return m.theme.Dimmed.Render(fitToWidth("h/l: scroll cols  0/$: first/last col", m.width))
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

	line := "◀" + string(track) + "▶"
	info := fmt.Sprintf(" %d/%d", m.scrollLeft+1, totalCols)
	if len([]rune(line))+len([]rune(info)) <= m.width {
		line += info
	}
	return m.theme.Dimmed.Render(fitToWidth(line, m.width))
}

// fitToWidth returns a single-line string that never exceeds width columns.
func fitToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) > width {
		r = r[:width]
	}
	return string(r)
}

func (m *Model) syncRangeSelection() {
	if !m.rangeSelect || m.result == nil {
		return
	}
	lo, hi := m.rangeAnchor, m.cursorRow
	if lo > hi {
		lo, hi = hi, lo
	}
	m.selectedRows = make(map[int]struct{})
	for r := lo; r <= hi; r++ {
		if r >= 0 && r < len(m.result.Rows) {
			m.selectedRows[r] = struct{}{}
		}
	}
}

func (m Model) isRowSelected(row int) bool {
	if m.selectedRows == nil {
		return false
	}
	_, ok := m.selectedRows[row]
	return ok
}

func (m *Model) deleteTargetRows() []int {
	if m.result == nil {
		return nil
	}
	if len(m.selectedRows) == 0 {
		if m.cursorRow >= 0 && m.cursorRow < len(m.result.Rows) {
			return []int{m.cursorRow}
		}
		return nil
	}
	idx := make([]int, 0, len(m.selectedRows))
	for r := range m.selectedRows {
		if r >= 0 && r < len(m.result.Rows) {
			idx = append(idx, r)
		}
	}
	sort.Ints(idx)
	return idx
}

func (m *Model) deleteDraftCmd() tea.Cmd {
	if m.result == nil || len(m.result.Rows) == 0 {
		return func() tea.Msg { return DeleteDraftMsg{Err: "No rows to delete."} }
	}
	rowsIdx := m.deleteTargetRows()
	if len(rowsIdx) == 0 {
		return func() tea.Msg { return DeleteDraftMsg{Err: "No rows to delete."} }
	}
	if strings.TrimSpace(m.result.SourceSQL) == "" {
		return func() tea.Msg {
			return DeleteDraftMsg{Err: "No source query recorded — run a SELECT first, then use d."}
		}
	}
	table, ok := sqlutil.TableFromSimpleSelect(m.result.SourceSQL)
	if !ok || table == "" {
		return func() tea.Msg {
			return DeleteDraftMsg{Err: "Can't infer table name — use a simple SELECT from one table (no WITH, JOIN, or subquery in FROM)."}
		}
	}
	var data [][]string
	for _, ri := range rowsIdx {
		row := m.result.Rows[ri]
		cp := make([]string, len(row))
		copy(cp, row)
		data = append(data, cp)
	}
	driver := m.result.Driver
	database := m.result.Database
	cols := append([]string(nil), m.result.Columns...)
	tbl := table
	return func() tea.Msg {
		return DeleteDraftRequestMsg{
			Driver:    driver,
			Database:  database,
			TableExpr: tbl,
			Columns:   cols,
			Rows:      data,
		}
	}
}

func (m *Model) openUpdatePopup() {
	if m.result == nil || len(m.result.Rows) == 0 {
		return
	}
	if m.cursorRow < 0 || m.cursorRow >= len(m.result.Rows) {
		return
	}
	val := m.SelectedCell()
	m.showUpdatePopup = true
	m.updateInput = []rune(val)
	m.updateCursorPos = len(m.updateInput)
}

func (m *Model) handleUpdatePopupKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.showUpdatePopup = false
		m.updateInput = nil
		m.updateCursorPos = 0
		return nil
	case "enter":
		cmd := m.updateDraftCmd()
		m.showUpdatePopup = false
		m.updateInput = nil
		m.updateCursorPos = 0
		return cmd
	case "backspace":
		if m.updateCursorPos > 0 {
			m.updateInput = append(m.updateInput[:m.updateCursorPos-1], m.updateInput[m.updateCursorPos:]...)
			m.updateCursorPos--
		}
		return nil
	case "delete":
		if m.updateCursorPos < len(m.updateInput) {
			m.updateInput = append(m.updateInput[:m.updateCursorPos], m.updateInput[m.updateCursorPos+1:]...)
		}
		return nil
	case "left":
		if m.updateCursorPos > 0 {
			m.updateCursorPos--
		}
		return nil
	case "right":
		if m.updateCursorPos < len(m.updateInput) {
			m.updateCursorPos++
		}
		return nil
	case "home", "ctrl+a":
		m.updateCursorPos = 0
		return nil
	case "end", "ctrl+e":
		m.updateCursorPos = len(m.updateInput)
		return nil
	}

	if len(msg.Runes) > 0 {
		for _, r := range msg.Runes {
			m.updateInput = append(m.updateInput[:m.updateCursorPos], append([]rune{r}, m.updateInput[m.updateCursorPos:]...)...)
			m.updateCursorPos++
		}
	}
	return nil
}

func (m *Model) updateDraftCmd() tea.Cmd {
	if m.result == nil || len(m.result.Rows) == 0 {
		return func() tea.Msg { return UpdateDraftMsg{Err: "No rows."} }
	}
	if strings.TrimSpace(m.result.SourceSQL) == "" {
		return func() tea.Msg {
			return UpdateDraftMsg{Err: "No source query — run a SELECT first, then use u."}
		}
	}
	table, ok := sqlutil.TableFromSimpleSelect(m.result.SourceSQL)
	if !ok || table == "" {
		return func() tea.Msg {
			return UpdateDraftMsg{Err: "Can't infer table name — use a simple SELECT from one table."}
		}
	}
	row := m.result.Rows[m.cursorRow]
	cp := make([]string, len(row))
	copy(cp, row)
	colName := m.result.Columns[m.cursorCol]
	newVal := string(m.updateInput)
	driver := m.result.Driver
	database := m.result.Database
	cols := append([]string(nil), m.result.Columns...)
	tbl := table
	return func() tea.Msg {
		return UpdateDraftRequestMsg{
			Driver:    driver,
			Database:  database,
			TableExpr: tbl,
			Columns:   cols,
			Row:       cp,
			ColName:   colName,
			NewValue:  newVal,
		}
	}
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
				if w := len([]rune(sanitizeTableCell(cell))); w > colWidths[i] {
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

func sanitizeTableCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " \\n ")
	s = strings.ReplaceAll(s, "\n", " \\n ")
	s = strings.ReplaceAll(s, "\r", " \\n ")
	return s
}

func (m Model) selectedCellLines() []string {
	val := m.SelectedCell()
	val = strings.ReplaceAll(val, "\r\n", "\n")
	val = strings.ReplaceAll(val, "\r", "\n")
	lines := strings.Split(val, "\n")
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func (m Model) renderCellPopup() string {
	boxW := m.width - 4
	boxH := m.height - 2
	if boxW < 20 {
		boxW = m.width
	}
	if boxH < 6 {
		boxH = m.height
	}
	innerW := boxW - 4
	innerH := boxH - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}

	lines := m.selectedCellLines()
	maxTop := 0
	if len(lines) > innerH {
		maxTop = len(lines) - innerH
	}
	if m.cellPopupTop > maxTop {
		m.cellPopupTop = maxTop
	}

	var sb strings.Builder
	for i := m.cellPopupTop; i < len(lines) && i < m.cellPopupTop+innerH; i++ {
		sb.WriteString(truncate(lines[i], innerW))
		sb.WriteString("\n")
	}
	if m.cellPopupMsg != "" {
		sb.WriteString("\n")
		sb.WriteString(m.theme.Success.Render(truncate(m.cellPopupMsg, innerW)))
		sb.WriteString("\n")
	}
	body := lipgloss.NewStyle().Width(innerW).Height(innerH).Render(sb.String())

	title := fmt.Sprintf("Cell Value (row %d col %d)", m.cursorRow+1, m.cursorCol+1)
	footer := "y copy  j/k scroll  g/G top/bottom  esc close"
	if maxTop == 0 {
		footer = "y copy  esc close"
	}
	popup := m.theme.BorderFocused.
		Width(boxW - 2).
		Height(boxH - 2).
		Render(body)
	popup = m.embedPopupBorderLabels(popup, title, footer)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup)
}

func (m Model) embedPopupBorderLabels(boxed, topLabel, bottomLabel string) string {
	lines := strings.Split(boxed, "\n")
	if len(lines) < 2 {
		return boxed
	}
	width := ansi.StringWidth(lines[0])
	lines[0] = m.renderPopupBorderLine(width, topLabel, true)
	lines[len(lines)-1] = m.renderPopupBorderLine(width, bottomLabel, false)
	return strings.Join(lines, "\n")
}

func (m Model) renderPopupBorderLine(width int, label string, top bool) string {
	if width < 3 {
		return strings.Repeat("─", max(0, width))
	}
	left := "╭"
	right := "╮"
	if !top {
		left = "╰"
		right = "╯"
	}
	sep := "─"
	mid := width - 2
	if mid < 1 {
		return lipgloss.NewStyle().Foreground(m.theme.BorderFocused.GetBorderTopForeground()).Render(left + right)
	}

	part := sep + " " + label + " "
	if ansi.StringWidth(part) > mid {
		inner := mid - ansi.StringWidth(sep+"  ")
		if inner < 1 {
			part = strings.Repeat(sep, mid)
		} else {
			part = sep + " " + ansi.Truncate(label, inner, "…") + " "
		}
	}
	fill := mid - ansi.StringWidth(part)
	if fill < 0 {
		fill = 0
	}
	var line string
	if top {
		line = left + part + strings.Repeat(sep, fill) + right
	} else {
		line = left + strings.Repeat(sep, fill) + part + right
	}
	style := lipgloss.NewStyle().Foreground(m.theme.BorderFocused.GetBorderTopForeground())
	return style.Render(line)
}

func (m Model) renderUpdatePopup() string {
	colName := ""
	if m.cursorCol >= 0 && m.cursorCol < len(m.result.Columns) {
		colName = m.result.Columns[m.cursorCol]
	}
	title := fmt.Sprintf("Update %s (row %d)", colName, m.cursorRow+1)
	footer := "Enter - confirm · Esc - cancel"

	boxW := m.width * 3 / 4
	if boxW < 40 {
		boxW = m.width - 4
	}
	if boxW < 10 {
		boxW = m.width
	}
	boxH := 5
	innerW := boxW - 4
	if innerW < 1 {
		innerW = 1
	}

	inputStr := string(m.updateInput)
	displayed := inputStr
	cursorPos := m.updateCursorPos

	if ansi.StringWidth(displayed) > innerW-1 {
		start := cursorPos - innerW/2
		if start < 0 {
			start = 0
		}
		end := start + innerW - 1
		runes := m.updateInput
		if end > len(runes) {
			end = len(runes)
			start = end - innerW + 1
			if start < 0 {
				start = 0
			}
		}
		displayed = string(runes[start:end])
		cursorPos = cursorPos - start
	}

	var sb strings.Builder
	sb.WriteString(displayed)
	dispW := ansi.StringWidth(displayed)
	if dispW < innerW {
		sb.WriteString(strings.Repeat(" ", innerW-dispW))
	}
	line := sb.String()

	runes := []rune(line)
	if cursorPos < 0 {
		cursorPos = 0
	}
	if cursorPos > len(runes) {
		cursorPos = len(runes)
	}
	before := string(runes[:cursorPos])
	cursorChar := " "
	if cursorPos < len(runes) {
		cursorChar = string(runes[cursorPos])
	}
	after := ""
	if cursorPos+1 < len(runes) {
		after = string(runes[cursorPos+1:])
	}

	cursorStyle := lipgloss.NewStyle().Reverse(true)
	body := before + cursorStyle.Render(cursorChar) + after

	popup := m.theme.BorderFocused.
		Width(boxW - 2).
		Height(boxH - 2).
		Render(lipgloss.NewStyle().Width(innerW).Render(body))
	popup = m.embedPopupBorderLabels(popup, title, footer)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup)
}
