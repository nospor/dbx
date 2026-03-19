package editor

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/robertn/dbx/internal/ui/theme"
	"github.com/robertn/dbx/internal/util"
)

// ExecuteQueryMsg is sent when the user requests query execution.
type ExecuteQueryMsg struct {
	Query string
}

// Model is the bubbletea model for the query editor panel.
type Model struct {
	theme   theme.Theme
	width   int
	height  int
	focused bool

	// Per-connection state: connKey -> lines
	tabs    map[string][]string
	connKey string

	// Vim state
	vim *VimState

	// Scroll offset (line index of top visible line)
	scrollTop int

	// Autocomplete
	completer   *CompletionProvider
	completions []string
	compCursor  int
	compVisible bool

	// History browsing
	history       []string
	histCursor    int
	histBrowsing  bool
}

// New creates a new editor model.
func New(t theme.Theme) Model {
	m := Model{
		theme:     t,
		tabs:      make(map[string][]string),
		connKey:   "",
		vim:       newVimState(),
		completer: NewCompletionProvider(),
	}
	m.ensureTab("")
	return m
}

// SetSchema updates the autocomplete schema tokens.
func (m *Model) SetSchema(tables, columns []string) {
	m.completer.SetSchema(tables, columns)
}

// SetHistory sets the history entries for the current connection (newest first).
func (m *Model) SetHistory(entries []string) {
	m.history = entries
	m.histCursor = -1
	m.histBrowsing = false
}

// BrowseHistoryPrev loads the previous history entry into the editor.
func (m *Model) BrowseHistoryPrev() {
	if len(m.history) == 0 {
		return
	}
	m.histBrowsing = true
	m.histCursor++
	if m.histCursor >= len(m.history) {
		m.histCursor = len(m.history) - 1
	}
	m.setLines(strings.Split(m.history[m.histCursor], "\n"))
	m.vim.row = len(m.lines()) - 1
	m.vim.col = 0
}

// BrowseHistoryNext loads the next history entry (or clears if at end).
func (m *Model) BrowseHistoryNext() {
	if !m.histBrowsing {
		return
	}
	m.histCursor--
	if m.histCursor < 0 {
		m.histBrowsing = false
		m.histCursor = -1
		m.setLines([]string{""})
	} else {
		m.setLines(strings.Split(m.history[m.histCursor], "\n"))
	}
	m.vim.row = len(m.lines()) - 1
	m.vim.col = 0
}

func (m *Model) ensureTab(key string) {
	if _, ok := m.tabs[key]; !ok {
		m.tabs[key] = []string{""}
	}
}

func (m *Model) lines() []string {
	return m.tabs[m.connKey]
}

func (m *Model) setLines(lines []string) {
	if len(lines) == 0 {
		lines = []string{""}
	}
	m.tabs[m.connKey] = lines
}

// SwitchConnection switches the editor to the tab for the given connection key.
func (m *Model) SwitchConnection(key string) {
	m.connKey = key
	m.ensureTab(key)
	m.vim = newVimState()
	m.scrollTop = 0
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

// IsInsertMode returns true when the editor is in vim insert mode.
func (m Model) IsInsertMode() bool {
	return m.vim.mode == ModeInsert
}

// CurrentQuery returns the query block under the cursor.
func (m Model) CurrentQuery() string {
	return m.currentQuery(m.lines())
}

// SetContent replaces the current tab content (e.g. when pressing 's' in explorer).
func (m *Model) SetContent(content string) {
	m.setLines(strings.Split(content, "\n"))
	m.vim.row = len(m.lines()) - 1
	m.vim.col = 0
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
	lines := m.lines()

	if m.vim.mode == ModeInsert {
		// Handle autocomplete navigation when visible
		if m.compVisible {
			switch msg.String() {
			case "esc":
				m.compVisible = false
				return nil
			case "tab", "ctrl+n":
				m.compCursor = (m.compCursor + 1) % len(m.completions)
				return nil
			case "ctrl+p":
				m.compCursor = (m.compCursor - 1 + len(m.completions)) % len(m.completions)
				return nil
			case "enter":
				// Accept completion
				lines = m.acceptCompletion(lines)
				m.setLines(lines)
				m.compVisible = false
				m.clampCursor()
				m.adjustScroll()
				return nil
			}
		}

		switch msg.String() {
		case "ctrl+p":
			m.BrowseHistoryPrev()
			return nil
		case "ctrl+n":
			m.BrowseHistoryNext()
			return nil
		case "esc":
			m.vim.mode = ModeNormal
			m.compVisible = false
			if m.vim.col > 0 {
				m.vim.col--
			}
		case "backspace":
			lines = m.deleteBackspace(lines)
			m.compVisible = false
		case "enter":
			lines = m.insertNewline(lines)
			m.compVisible = false
		case "tab":
			// Trigger autocomplete
			prefix := m.wordBeforeCursor(lines)
			if prefix != "" {
				m.completions = m.completer.Complete(prefix, 8)
				if len(m.completions) > 0 {
					m.compCursor = 0
					m.compVisible = true
				}
			}
	case "ctrl+enter", "ctrl+r", "f5":
		q := m.currentQuery(lines)
		m.setLines(lines)
		m.compVisible = false
		return func() tea.Msg { return ExecuteQueryMsg{Query: q} }
		case "ctrl+v":
			if text, err := util.Paste(); err == nil && text != "" {
				for _, ch := range text {
					if ch == '\n' {
						lines = m.insertNewline(lines)
					} else if ch != '\r' {
						lines = m.insertRune(lines, ch)
					}
				}
				m.compVisible = false
			}
		default:
			if len(msg.Runes) == 1 {
				lines = m.insertRune(lines, msg.Runes[0])
				m.compVisible = false
			}
		}
		m.setLines(lines)
		m.clampCursor()
		m.adjustScroll()
		return nil
	}

	// Normal mode
	switch msg.String() {
	case "i":
		m.vim.mode = ModeInsert
	case "I":
		m.vim.col = 0
		m.vim.mode = ModeInsert
	case "a":
		if len(lines) > 0 && m.vim.col < len([]rune(lines[m.vim.row])) {
			m.vim.col++
		}
		m.vim.mode = ModeInsert
	case "A":
		if len(lines) > 0 {
			m.vim.col = len([]rune(lines[m.vim.row]))
		}
		m.vim.mode = ModeInsert
	case "o":
		lines = m.openLineBelow(lines)
		m.setLines(lines)
		m.vim.mode = ModeInsert
	case "O":
		lines = m.openLineAbove(lines)
		m.setLines(lines)
		m.vim.mode = ModeInsert
	case "h", "left":
		if m.vim.col > 0 {
			m.vim.col--
		}
	case "l", "right":
		if len(lines) > 0 && m.vim.col < len([]rune(lines[m.vim.row]))-1 {
			m.vim.col++
		}
	case "j", "down":
		if m.vim.row < len(lines)-1 {
			m.vim.row++
			m.clampCol(lines)
		}
	case "k", "up":
		if m.vim.row > 0 {
			m.vim.row--
			m.clampCol(lines)
		}
	case "0":
		m.vim.col = 0
	case "$":
		if len(lines) > 0 {
			l := len([]rune(lines[m.vim.row]))
			if l > 0 {
				m.vim.col = l - 1
			}
		}
	case "g":
		if m.vim.pendingG {
			m.vim.row = 0
			m.vim.col = 0
			m.vim.pendingG = false
		} else {
			m.vim.pendingG = true
		}
	case "G":
		m.vim.row = len(lines) - 1
		m.vim.col = 0
	case "w":
		m.vim.row, m.vim.col = wordForward(lines, m.vim.row, m.vim.col)
	case "b":
		m.vim.row, m.vim.col = wordBackward(lines, m.vim.row, m.vim.col)
	case "x":
		lines = m.deleteCharAt(lines)
		m.setLines(lines)
	case "d":
		if m.vim.pendingD {
			lines = m.deleteLine(lines)
			m.setLines(lines)
			m.vim.pendingD = false
		} else {
			m.vim.pendingD = true
		}
	case "enter", "ctrl+enter", "ctrl+r", "f5":
		q := m.currentQuery(lines)
		return func() tea.Msg { return ExecuteQueryMsg{Query: q} }
	case "ctrl+p":
		m.BrowseHistoryPrev()
		return nil
	case "ctrl+n":
		if m.histBrowsing {
			m.BrowseHistoryNext()
		}
		return nil
	default:
		m.vim.pendingG = false
		m.vim.pendingD = false
	}

	m.clampCursor()
	m.adjustScroll()
	return nil
}

func (m *Model) clampCursor() {
	lines := m.lines()
	if len(lines) == 0 {
		m.vim.row = 0
		m.vim.col = 0
		return
	}
	if m.vim.row >= len(lines) {
		m.vim.row = len(lines) - 1
	}
	if m.vim.row < 0 {
		m.vim.row = 0
	}
	lineLen := len([]rune(lines[m.vim.row]))
	maxCol := lineLen - 1
	if m.vim.mode == ModeInsert {
		maxCol = lineLen
	}
	if maxCol < 0 {
		maxCol = 0
	}
	if m.vim.col > maxCol {
		m.vim.col = maxCol
	}
	if m.vim.col < 0 {
		m.vim.col = 0
	}
}

func (m *Model) clampCol(lines []string) {
	if m.vim.row >= len(lines) {
		return
	}
	lineLen := len([]rune(lines[m.vim.row]))
	if lineLen == 0 {
		m.vim.col = 0
		return
	}
	if m.vim.col >= lineLen {
		m.vim.col = lineLen - 1
	}
}

func (m *Model) adjustScroll() {
	visibleLines := m.height - 3
	if visibleLines < 1 {
		visibleLines = 1
	}
	if m.vim.row < m.scrollTop {
		m.scrollTop = m.vim.row
	}
	if m.vim.row >= m.scrollTop+visibleLines {
		m.scrollTop = m.vim.row - visibleLines + 1
	}
}

func (m *Model) insertRune(lines []string, r rune) []string {
	if len(lines) == 0 {
		lines = []string{""}
	}
	row := m.vim.row
	runes := []rune(lines[row])
	col := m.vim.col
	if col > len(runes) {
		col = len(runes)
	}
	runes = append(runes[:col], append([]rune{r}, runes[col:]...)...)
	lines[row] = string(runes)
	m.vim.col = col + 1
	return lines
}

func (m *Model) deleteBackspace(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	row, col := m.vim.row, m.vim.col
	if col > 0 {
		runes := []rune(lines[row])
		runes = append(runes[:col-1], runes[col:]...)
		lines[row] = string(runes)
		m.vim.col--
	} else if row > 0 {
		prevLen := len([]rune(lines[row-1]))
		lines[row-1] = lines[row-1] + lines[row]
		lines = append(lines[:row], lines[row+1:]...)
		m.vim.row--
		m.vim.col = prevLen
	}
	return lines
}

func (m *Model) insertNewline(lines []string) []string {
	row, col := m.vim.row, m.vim.col
	runes := []rune(lines[row])
	before := string(runes[:col])
	after := string(runes[col:])
	lines[row] = before
	newLines := make([]string, len(lines)+1)
	copy(newLines, lines[:row+1])
	newLines[row+1] = after
	copy(newLines[row+2:], lines[row+1:])
	m.vim.row++
	m.vim.col = 0
	return newLines
}

func (m *Model) openLineBelow(lines []string) []string {
	row := m.vim.row
	newLines := make([]string, len(lines)+1)
	copy(newLines, lines[:row+1])
	newLines[row+1] = ""
	copy(newLines[row+2:], lines[row+1:])
	m.vim.row++
	m.vim.col = 0
	return newLines
}

func (m *Model) openLineAbove(lines []string) []string {
	row := m.vim.row
	newLines := make([]string, len(lines)+1)
	copy(newLines, lines[:row])
	newLines[row] = ""
	copy(newLines[row+1:], lines[row:])
	m.vim.col = 0
	return newLines
}

func (m *Model) deleteCharAt(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	row, col := m.vim.row, m.vim.col
	runes := []rune(lines[row])
	if col >= len(runes) {
		return lines
	}
	runes = append(runes[:col], runes[col+1:]...)
	lines[row] = string(runes)
	if m.vim.col >= len(runes) && m.vim.col > 0 {
		m.vim.col--
	}
	return lines
}

func (m *Model) deleteLine(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	row := m.vim.row
	lines = append(lines[:row], lines[row+1:]...)
	if len(lines) == 0 {
		lines = []string{""}
	}
	if m.vim.row >= len(lines) {
		m.vim.row = len(lines) - 1
	}
	return lines
}

// wordBeforeCursor returns the partial word immediately before the cursor.
func (m *Model) wordBeforeCursor(lines []string) string {
	if len(lines) == 0 || m.vim.row >= len(lines) {
		return ""
	}
	runes := []rune(lines[m.vim.row])
	col := m.vim.col
	if col > len(runes) {
		col = len(runes)
	}
	end := col
	start := end
	for start > 0 && isWordRune(runes[start-1]) {
		start--
	}
	return string(runes[start:end])
}

func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// acceptCompletion replaces the partial word before the cursor with the selected completion.
func (m *Model) acceptCompletion(lines []string) []string {
	if len(m.completions) == 0 || m.compCursor >= len(m.completions) {
		return lines
	}
	if len(lines) == 0 || m.vim.row >= len(lines) {
		return lines
	}
	runes := []rune(lines[m.vim.row])
	col := m.vim.col
	if col > len(runes) {
		col = len(runes)
	}
	end := col
	start := end
	for start > 0 && isWordRune(runes[start-1]) {
		start--
	}
	completion := []rune(m.completions[m.compCursor])
	newRunes := make([]rune, 0, len(runes)-( end-start)+len(completion))
	newRunes = append(newRunes, runes[:start]...)
	newRunes = append(newRunes, completion...)
	newRunes = append(newRunes, runes[end:]...)
	lines[m.vim.row] = string(newRunes)
	m.vim.col = start + len(completion)
	return lines
}

// currentQuery returns the query block that the cursor is currently in.
// Queries are separated by at least one blank line.
// If the cursor is on a blank line, the block above (if any) is used.
func (m *Model) currentQuery(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	row := m.vim.row
	if row >= len(lines) {
		row = len(lines) - 1
	}

	// If cursor is on a blank line, use the preceding non-empty block
	start := row
	for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	end := row
	for end < len(lines)-1 && strings.TrimSpace(lines[end+1]) != "" {
		end++
	}
	q := strings.TrimSpace(strings.Join(lines[start:end+1], "\n"))
	if q != "" {
		return q
	}
	// Cursor on blank line: find the block above
	if row > 0 {
		above := row - 1
		for above > 0 && strings.TrimSpace(lines[above-1]) != "" {
			above--
		}
		return strings.TrimSpace(strings.Join(lines[above:row], "\n"))
	}
	return ""
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	lines := m.lines()
	visibleLines := m.height - 3 // title + status bar + border

	var sb strings.Builder

	// Title bar
	modeStr := m.theme.StatusBarMode.Render(" " + m.vim.mode.String() + " ")
	title := m.theme.StatusBar.Width(m.width - lipgloss.Width(modeStr) - 2).Render("Query Editor")
	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, modeStr, title) + "\n")

	// Render visible lines
	for i := m.scrollTop; i < m.scrollTop+visibleLines; i++ {
		var lineStr string
		if i < len(lines) {
			lineStr = lines[i]
		}

		lineRunes := []rune(lineStr)
		rendered := renderHighlighted(lineStr, m.theme)

		// Draw cursor
		if i == m.vim.row && m.focused {
			col := m.vim.col
			if col > len(lineRunes) {
				col = len(lineRunes)
			}
			before := string(lineRunes[:col])
			var cursorChar string
			if col < len(lineRunes) {
				cursorChar = string(lineRunes[col])
			} else {
				cursorChar = " "
			}
			after := ""
			if col+1 < len(lineRunes) {
				after = string(lineRunes[col+1:])
			}
			_ = rendered
			rendered = before + lipgloss.NewStyle().Reverse(true).Render(cursorChar) + after
		}

		sb.WriteString(lipgloss.NewStyle().Width(m.width).Render(rendered) + "\n")
	}

	// Render autocomplete dropdown
	if m.compVisible && len(m.completions) > 0 {
		var compSb strings.Builder
		for i, c := range m.completions {
			if i == m.compCursor {
				compSb.WriteString(lipgloss.NewStyle().Reverse(true).Render(" "+c+" ") + "\n")
			} else {
				compSb.WriteString(" " + c + " \n")
			}
		}
		compBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("12")).
			Render(compSb.String())

		// Position the dropdown below the cursor line
		cursorScreenRow := m.vim.row - m.scrollTop + 2
		result := sb.String()
		resultLines := strings.Split(result, "\n")
		compLines := strings.Split(compBox, "\n")
		insertRow := cursorScreenRow + 1
		insertCol := m.vim.col + 2
		for i, cl := range compLines {
			row := insertRow + i
			if row >= len(resultLines) {
				break
			}
			line := []rune(resultLines[row])
			for len(line) < insertCol+lipgloss.Width(cl) {
				line = append(line, ' ')
			}
			clRunes := []rune(cl)
			for j, r := range clRunes {
				pos := insertCol + j
				if pos < len(line) {
					line[pos] = r
				}
			}
			resultLines[row] = string(line)
		}
		return strings.Join(resultLines, "\n")
	}

	return sb.String()
}

// wordForward moves to the start of the next word.
func wordForward(lines []string, row, col int) (int, int) {
	if row >= len(lines) {
		return row, col
	}
	runes := []rune(lines[row])
	col++
	for col < len(runes) && runes[col] != ' ' {
		col++
	}
	for col < len(runes) && runes[col] == ' ' {
		col++
	}
	if col >= len(runes) {
		if row < len(lines)-1 {
			return row + 1, 0
		}
		if len(runes) > 0 {
			col = len(runes) - 1
		}
	}
	return row, col
}

// wordBackward moves to the start of the previous word.
func wordBackward(lines []string, row, col int) (int, int) {
	if row >= len(lines) {
		return row, col
	}
	runes := []rune(lines[row])
	col--
	if col < 0 {
		if row > 0 {
			row--
			runes = []rune(lines[row])
			col = len(runes) - 1
		} else {
			return 0, 0
		}
	}
	for col > 0 && runes[col] == ' ' {
		col--
	}
	for col > 0 && runes[col-1] != ' ' {
		col--
	}
	return row, col
}
