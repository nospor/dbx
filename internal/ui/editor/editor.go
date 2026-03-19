package editor

import (
	"fmt"
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

// DeleteHistoryEntryMsg asks the app to remove a query from persisted history.
type DeleteHistoryEntryMsg struct {
	Query string
}

// QueryPanePersistMsg asks the app to persist the editor buffer for the given connection key.
// ConnKey is the logical key (may be "" before a DB is selected); the app maps "" to "_".
type QueryPanePersistMsg struct {
	ConnKey string
	Text    string
}

func tabStoreKey(connKey string) string {
	if connKey == "" {
		return "_"
	}
	return connKey
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

	// History popup
	histPopupVisible            bool
	histPopupCursor             int
	histPopupPendingDeleteQuery string // non-empty = showing delete confirmation for this exact query
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

// SeedQueryTabs hydrates in-memory tabs from disk (e.g. at startup).
func (m *Model) SeedQueryTabs(contents map[string]string) {
	if len(contents) == 0 {
		return
	}
	for key, text := range contents {
		sk := tabStoreKey(key)
		m.tabs[sk] = linesFromText(text)
	}
}

func linesFromText(text string) []string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// SetSchema updates the autocomplete schema tokens.
func (m *Model) SetSchema(tables, columns []string) {
	m.completer.SetSchema(tables, columns)
}

// SetHistory sets the history entries for the current connection (newest first).
// Closes the history popup (e.g. when switching database).
func (m *Model) SetHistory(entries []string) {
	m.history = entries
	m.histCursor = -1
	m.histBrowsing = false
	m.histPopupVisible = false
	m.histPopupCursor = 0
	m.histPopupPendingDeleteQuery = ""
}

// ReplaceHistoryEntries updates the in-memory history list without closing the popup.
func (m *Model) ReplaceHistoryEntries(entries []string) {
	m.history = entries
	if len(entries) == 0 {
		m.histPopupVisible = false
		m.histPopupCursor = 0
		m.histPopupPendingDeleteQuery = ""
		return
	}
	if m.histPopupCursor >= len(entries) {
		m.histPopupCursor = len(entries) - 1
	}
}

// acceptHistoryPopup appends the selected history entry to the editor content.
func (m *Model) acceptHistoryPopup() {
	if !m.histPopupVisible || len(m.history) == 0 {
		return
	}
	if m.histPopupCursor >= len(m.history) {
		m.histPopupCursor = 0
	}
	existing := m.lines()
	// Strip trailing blank lines from existing content
	for len(existing) > 0 && strings.TrimSpace(existing[len(existing)-1]) == "" {
		existing = existing[:len(existing)-1]
	}
	newLines := strings.Split(m.history[m.histPopupCursor], "\n")
	// Separate with a blank line if there's existing content
	if len(existing) > 0 {
		existing = append(existing, "")
	}
	combined := append(existing, newLines...)
	m.setLines(combined)
	m.vim.row = len(m.lines()) - 1
	m.vim.col = 0
	m.histPopupVisible = false
	m.histPopupPendingDeleteQuery = ""
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
	sk := tabStoreKey(key)
	if _, ok := m.tabs[sk]; !ok {
		m.tabs[sk] = []string{""}
	}
}

func (m *Model) lines() []string {
	return m.tabs[tabStoreKey(m.connKey)]
}

func (m *Model) setLines(lines []string) {
	if len(lines) == 0 {
		lines = []string{""}
	}
	m.tabs[tabStoreKey(m.connKey)] = lines
}

// SwitchConnection switches the editor to the tab for the given connection key.
func (m *Model) SwitchConnection(key string) {
	m.connKey = key
	m.ensureTab(key)
	m.vim = newVimState()
	m.scrollTop = 0
}

// EditorConnKey returns the logical connection key for the active tab (may be "").
func (m Model) EditorConnKey() string {
	return m.connKey
}

// TabText returns the full buffer for the active tab.
func (m Model) TabText() string {
	return strings.Join(m.lines(), "\n")
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
			case "tab", "ctrl+n", "down":
				m.compCursor = (m.compCursor + 1) % len(m.completions)
				return nil
			case "ctrl+p", "up":
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
			m.setLines(lines)
			m.clampCursor()
			m.adjustScroll()
			persistKey := m.connKey
			persistText := strings.Join(lines, "\n")
			return func() tea.Msg {
				return QueryPanePersistMsg{ConnKey: persistKey, Text: persistText}
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
		case "left":
			if m.vim.col > 0 {
				m.vim.col--
			} else if m.vim.row > 0 {
				m.vim.row--
				if len(lines) > 0 && m.vim.row < len(lines) {
					m.vim.col = len([]rune(lines[m.vim.row]))
				}
			}
			m.compVisible = false
		case "right":
			if len(lines) == 0 {
				break
			}
			runes := []rune(lines[m.vim.row])
			if m.vim.col < len(runes) {
				m.vim.col++
			} else if m.vim.row < len(lines)-1 {
				m.vim.row++
				m.vim.col = 0
			}
			m.compVisible = false
		case "up":
			if m.vim.row > 0 {
				m.vim.row--
				m.clampCol(lines)
			}
			m.compVisible = false
		case "down":
			if m.vim.row < len(lines)-1 {
				m.vim.row++
				m.clampCol(lines)
			}
			m.compVisible = false
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
		if m.histPopupVisible {
			if m.histPopupPendingDeleteQuery != "" {
				return nil
			}
			if m.histPopupCursor < len(m.history)-1 {
				m.histPopupCursor++
			}
			return nil
		}
		if m.vim.row < len(lines)-1 {
			m.vim.row++
			m.clampCol(lines)
		}
	case "k", "up":
		if m.histPopupVisible {
			if m.histPopupPendingDeleteQuery != "" {
				return nil
			}
			if m.histPopupCursor > 0 {
				m.histPopupCursor--
			}
			return nil
		}
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
		if m.histPopupVisible && m.histPopupPendingDeleteQuery != "" {
			m.histPopupPendingDeleteQuery = ""
			m.vim.pendingD = false
			return nil
		}
		if m.histPopupVisible && m.histPopupCursor < len(m.history) {
			m.histPopupPendingDeleteQuery = m.history[m.histPopupCursor]
			m.vim.pendingD = false
			return nil
		}
		if m.vim.pendingD {
			lines = m.deleteLine(lines)
			m.setLines(lines)
			m.vim.pendingD = false
		} else {
			m.vim.pendingD = true
		}
	case "y":
		if m.histPopupVisible && m.histPopupPendingDeleteQuery != "" {
			q := m.histPopupPendingDeleteQuery
			m.histPopupPendingDeleteQuery = ""
			return func() tea.Msg { return DeleteHistoryEntryMsg{Query: q} }
		}
	case "n":
		if m.histPopupVisible {
			m.histPopupPendingDeleteQuery = ""
			return nil
		}
	case "enter", "ctrl+enter", "ctrl+r", "f5":
		if m.histPopupVisible {
			if m.histPopupPendingDeleteQuery != "" {
				return nil
			}
			m.acceptHistoryPopup()
			return nil
		}
		q := m.currentQuery(lines)
		return func() tea.Msg { return ExecuteQueryMsg{Query: q} }
	case "backspace":
		if len(m.history) > 0 {
			m.histPopupVisible = true
			m.histPopupCursor = 0
			m.histPopupPendingDeleteQuery = ""
		}
		return nil
	case "esc":
		if m.histPopupVisible {
			if m.histPopupPendingDeleteQuery != "" {
				m.histPopupPendingDeleteQuery = ""
			} else {
				m.histPopupVisible = false
			}
			return nil
		}
	case "ctrl+p":
		if m.histPopupVisible {
			if m.histPopupPendingDeleteQuery != "" {
				return nil
			}
			if m.histPopupCursor > 0 {
				m.histPopupCursor--
			}
			return nil
		}
		m.BrowseHistoryPrev()
		return nil
	case "ctrl+n":
		if m.histPopupVisible {
			if m.histPopupPendingDeleteQuery != "" {
				return nil
			}
			if m.histPopupCursor < len(m.history)-1 {
				m.histPopupCursor++
			}
			return nil
		}
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
	// 1 mode row + (height-3) editor rows = height-2 rows (same as before the border title change).
	visibleLines := m.height - 3
	if visibleLines < 1 {
		visibleLines = 1
	}

	var sb strings.Builder

	// Mode indicator row (panel name + DB live on the app border).
	modeStr := m.theme.StatusBarMode.Render(" " + m.vim.mode.String() + " ")
	restW := m.width - lipgloss.Width(modeStr)
	if restW < 0 {
		restW = 0
	}
	modeRow := lipgloss.JoinHorizontal(lipgloss.Top, modeStr, lipgloss.NewStyle().Width(restW).Render(""))
	sb.WriteString(modeRow + "\n")

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

	// Render history popup — replaces the content area, keeps the title bar
	if m.histPopupVisible && len(m.history) > 0 {
		innerW := m.width - 2
		if innerW < 10 {
			innerW = 10
		}

		var popSb strings.Builder

		if m.histPopupPendingDeleteQuery != "" {
			// Delete confirmation: full-width sub-panel (no list navigation)
			topHint := m.theme.Dimmed.Width(m.width).Render(" Remove from history — y confirm   n / esc cancel ")
			popSb.WriteString(topHint + "\n")

			preview := strings.ReplaceAll(m.histPopupPendingDeleteQuery, "\n", " ↵ ")
			textW := innerW - 4
			if textW < 8 {
				textW = 8
			}
			previewLines := wrapRunesToWidth([]rune(preview), textW)
			maxPreview := visibleLines - 5
			if maxPreview < 2 {
				maxPreview = 2
			}
			if len(previewLines) > maxPreview {
				previewLines = previewLines[:maxPreview]
				previewLines = append(previewLines, "...")
			}
			boxInner := lipgloss.NewStyle().Bold(true).Render("Delete this query?") + "\n\n" +
				strings.Join(previewLines, "\n")
			box := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("9")).
				Width(innerW).
				Padding(0, 1).
				Render(boxInner)
			for _, bl := range strings.Split(box, "\n") {
				popSb.WriteString(lipgloss.NewStyle().Width(m.width).Render(bl) + "\n")
			}
		} else {
			maxVisible := visibleLines - 2
			if maxVisible < 1 {
				maxVisible = 1
			}

			start := 0
			if m.histPopupCursor >= maxVisible {
				start = m.histPopupCursor - maxVisible + 1
			}
			end := start + maxVisible
			if end > len(m.history) {
				end = len(m.history)
			}

			headerText := fmt.Sprintf(" History (%d)  j/k navigate  enter insert  esc close  d delete", len(m.history))
			header := m.theme.Dimmed.Width(m.width).Render(headerText)
			popSb.WriteString(header + "\n")

			for i := start; i < end; i++ {
				preview := strings.ReplaceAll(m.history[i], "\n", " ↵ ")
				runes := []rune(preview)
				if len(runes) > innerW-2 {
					preview = string(runes[:innerW-5]) + "..."
				}
				line := " " + preview
				if i == m.histPopupCursor {
					popSb.WriteString(m.theme.TreeSelected.Width(m.width).Render(line) + "\n")
				} else {
					popSb.WriteString(lipgloss.NewStyle().Width(m.width).Render(line) + "\n")
				}
			}
		}

		// Pad remaining lines
		rendered := popSb.String()
		renderedLines := strings.Count(rendered, "\n")
		for renderedLines < visibleLines {
			rendered += lipgloss.NewStyle().Width(m.width).Render("") + "\n"
			renderedLines++
		}

		titleLine := strings.SplitN(sb.String(), "\n", 2)[0]
		return titleLine + "\n" + rendered
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
		cursorScreenRow := m.vim.row - m.scrollTop + 1
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

// wrapRunesToWidth splits runes into lines of at most width runes each.
func wrapRunesToWidth(runes []rune, width int) []string {
	if width < 1 {
		width = 1
	}
	var lines []string
	for len(runes) > 0 {
		if len(runes) <= width {
			lines = append(lines, string(runes))
			break
		}
		lines = append(lines, string(runes[:width]))
		runes = runes[width:]
	}
	return lines
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
