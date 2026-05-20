package editor

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"

	"github.com/robertn/dbx/internal/sqlutil"
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

// TabSwitchedMsg notifies the app that the active editor tab changed (keyboard or close).
// ConnKey is "" when all tabs are closed (idle / no active database).
type TabSwitchedMsg struct {
	ConnKey string
}

var lineNumRegex = regexp.MustCompile(`^\s*\d+\s*[|│]`)

func tabStoreKey(connKey string) string {
	if connKey == "" {
		return "_"
	}
	return connKey
}

// editorTopGutterLines is blank rows between the tab bar and query text.
const editorTopGutterLines = 1

// completionPopupEdgeMargin is extra bottom (and placement) slack so the bordered popup
// is not clipped: the editor View ends with a newline-split empty line, and lipgloss borders
// need the full last row merged into real buffer lines.
const completionPopupEdgeMargin = 2

// Model is the bubbletea model for the query editor panel.
type Model struct {
	theme   theme.Theme
	width   int
	height  int
	focused bool

	// Per-connection state: connKey -> lines
	tabs    map[string][]string
	connKey string

	// Visible tabs (ordered). When empty, editor is idle (connKey "").
	openTabs     []TabInfo
	activeTabIdx int

	// Vim state
	vim *VimState

	// Scroll offset: first visible terminal row in the wrapped document (not logical buffer line).
	scrollTop int

	// Autocomplete
	completer   *CompletionProvider
	completions []string
	compCursor  int
	compVisible bool

	// History browsing
	history      []string
	histCursor   int
	histBrowsing bool

	// History popup
	histPopupVisible            bool
	histPopupCursor             int
	histPopupPendingDeleteQuery string // non-empty = showing delete confirmation for this exact query
	histPopupFilter             string // substring filter (case-insensitive); typing while popup is open
	histPopupFilteredIdx        []int  // indices into history matching histPopupFilter (newest-first order preserved)

	// Per-tab undo/redo (see undo.go)
	tabUndo          map[string]*tabUndoState
	skipUndoRecord   bool
	insertUndoSeeded bool // insert-session checkpoint taken (reset on Esc; see beforeInsertEdit)
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
	m.switchToIdle()
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
	m.closeHistoryPopup()
}

func (m *Model) closeHistoryPopup() {
	m.histPopupVisible = false
	m.histPopupCursor = 0
	m.histPopupPendingDeleteQuery = ""
	m.histPopupFilter = ""
	m.histPopupFilteredIdx = nil
}

func (m *Model) rebuildHistoryPopupFilter() {
	needle := strings.ToLower(m.histPopupFilter)
	m.histPopupFilteredIdx = m.histPopupFilteredIdx[:0]
	for i, h := range m.history {
		if needle == "" || strings.Contains(strings.ToLower(h), needle) {
			m.histPopupFilteredIdx = append(m.histPopupFilteredIdx, i)
		}
	}
	if len(m.histPopupFilteredIdx) == 0 {
		m.histPopupCursor = 0
	} else if m.histPopupCursor >= len(m.histPopupFilteredIdx) {
		m.histPopupCursor = len(m.histPopupFilteredIdx) - 1
	}
}

// ReplaceHistoryEntries updates the in-memory history list without closing the popup.
func (m *Model) ReplaceHistoryEntries(entries []string) {
	m.history = entries
	if len(entries) == 0 {
		m.closeHistoryPopup()
		return
	}
	if m.histPopupVisible {
		m.rebuildHistoryPopupFilter()
	}
}

// acceptHistoryPopup appends the selected history entry to the editor content.
func (m *Model) acceptHistoryPopup() {
	if !m.histPopupVisible || len(m.history) == 0 || len(m.histPopupFilteredIdx) == 0 {
		return
	}
	if m.histPopupCursor >= len(m.histPopupFilteredIdx) {
		m.histPopupCursor = 0
	}
	histIdx := m.histPopupFilteredIdx[m.histPopupCursor]
	existing := m.lines()
	// Strip trailing blank lines from existing content
	for len(existing) > 0 && strings.TrimSpace(existing[len(existing)-1]) == "" {
		existing = existing[:len(existing)-1]
	}
	newLines := strings.Split(m.history[histIdx], "\n")
	// Separate with a blank line if there's existing content
	if len(existing) > 0 {
		existing = append(existing, "")
	}
	combined := append(existing, newLines...)
	m.pushUndoPoint()
	m.setLines(combined)
	m.vim.row = len(m.lines()) - 1
	m.vim.col = 0
	m.closeHistoryPopup()
}

// BrowseHistoryPrev loads the previous history entry into the editor.
func (m *Model) BrowseHistoryPrev() {
	if len(m.history) == 0 {
		return
	}
	var next int
	if !m.histBrowsing {
		next = 0
	} else {
		next = m.histCursor + 1
	}
	if next >= len(m.history) {
		next = len(m.history) - 1
	}
	if m.histBrowsing && next == m.histCursor {
		return // already showing the oldest entry
	}
	m.pushUndoPoint()
	m.histBrowsing = true
	m.histCursor = next
	m.setLines(strings.Split(m.history[m.histCursor], "\n"))
	m.vim.row = len(m.lines()) - 1
	m.vim.col = 0
	if m.vim.mode == ModeInsert {
		m.insertUndoSeeded = true
	}
}

// BrowseHistoryNext loads the next history entry (or clears if at end).
func (m *Model) BrowseHistoryNext() {
	if !m.histBrowsing {
		return
	}
	m.pushUndoPoint()
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
	if m.vim.mode == ModeInsert {
		m.insertUndoSeeded = true
	}
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
	m.insertUndoSeeded = false
}

func (m *Model) switchToIdle() {
	m.openTabs = nil
	m.activeTabIdx = 0
	m.connKey = ""
	m.ensureTab("")
	m.vim = newVimState()
	m.scrollTop = 0
	m.insertUndoSeeded = false
	m.compVisible = false
	m.closeHistoryPopup()
	m.histBrowsing = false
	m.histCursor = -1
}

func (m *Model) activateTabIndex(i int) {
	if i < 0 || i >= len(m.openTabs) {
		return
	}
	m.activeTabIdx = i
	m.connKey = m.openTabs[i].ConnKey
	m.ensureTab(m.connKey)
	m.vim = newVimState()
	m.scrollTop = 0
	m.insertUndoSeeded = false
	m.compVisible = false
	m.closeHistoryPopup()
	m.histBrowsing = false
	m.histCursor = -1
}

// RestoreOpenTabs rebuilds visible tabs from persisted keys (after SeedQueryTabs).
// activeKey is the connID:database key for the tab that should be selected; if empty or not found, the first tab is activated.
func (m *Model) RestoreOpenTabs(keys []string, activeKey string, labelFor func(string) string) {
	m.openTabs = m.openTabs[:0]
	for _, k := range keys {
		if k == "" || k == "_" {
			continue
		}
		lbl := ""
		if labelFor != nil {
			lbl = labelFor(k)
		}
		if lbl == "" {
			lbl = k
		}
		m.openTabs = append(m.openTabs, TabInfo{ConnKey: k, Label: lbl})
		m.ensureTab(k)
	}
	if len(m.openTabs) == 0 {
		m.switchToIdle()
		return
	}
	idx := 0
	if activeKey != "" {
		for i, t := range m.openTabs {
			if t.ConnKey == activeKey {
				idx = i
				break
			}
		}
	}
	m.activateTabIndex(idx)
}

// OpenTab opens or activates a tab for the given connection key.
func (m *Model) OpenTab(connKey, label string) {
	if connKey == "" {
		return
	}
	for i, t := range m.openTabs {
		if t.ConnKey == connKey {
			if label != "" {
				m.openTabs[i].Label = label
			}
			m.activateTabIndex(i)
			return
		}
	}
	m.openTabs = append(m.openTabs, TabInfo{ConnKey: connKey, Label: label})
	m.activateTabIndex(len(m.openTabs) - 1)
}

// CloseActiveTab removes the current tab. Returns a message for the app if the connection changed.
func (m *Model) CloseActiveTab() *TabSwitchedMsg {
	if len(m.openTabs) == 0 {
		return nil
	}
	idx := m.activeTabIdx
	if idx < 0 || idx >= len(m.openTabs) {
		idx = len(m.openTabs) - 1
	}
	m.openTabs = append(m.openTabs[:idx], m.openTabs[idx+1:]...)
	if len(m.openTabs) == 0 {
		m.switchToIdle()
		return &TabSwitchedMsg{ConnKey: ""}
	}
	if m.activeTabIdx >= len(m.openTabs) {
		m.activeTabIdx = len(m.openTabs) - 1
	}
	m.activateTabIndex(m.activeTabIdx)
	return &TabSwitchedMsg{ConnKey: m.connKey}
}

// CycleTab moves the active tab by delta (-1 or +1). Returns nil if unchanged.
func (m *Model) CycleTab(delta int) *TabSwitchedMsg {
	if len(m.openTabs) < 2 {
		return nil
	}
	m.activeTabIdx = (m.activeTabIdx + delta) % len(m.openTabs)
	if m.activeTabIdx < 0 {
		m.activeTabIdx += len(m.openTabs)
	}
	m.activateTabIndex(m.activeTabIdx)
	return &TabSwitchedMsg{ConnKey: m.connKey}
}

// OpenTabKeys returns conn keys in tab order for persistence.
func (m Model) OpenTabKeys() []string {
	if len(m.openTabs) == 0 {
		return nil
	}
	out := make([]string, len(m.openTabs))
	for i, t := range m.openTabs {
		out[i] = t.ConnKey
	}
	return out
}

// EditorConnKey returns the logical connection key for the active tab (may be "").
func (m Model) EditorConnKey() string {
	return m.connKey
}

// TabText returns the full buffer for the active tab.
func (m Model) TabText() string {
	return strings.Join(m.lines(), "\n")
}

func (m *Model) queryPanePersistCmd() tea.Cmd {
	persistKey := m.connKey
	persistText := m.TabText()
	return func() tea.Msg {
		return QueryPanePersistMsg{ConnKey: persistKey, Text: persistText}
	}
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

// HistoryPopupVisible returns true while the query history overlay is open (filtering or delete confirm).
func (m Model) HistoryPopupVisible() bool {
	return m.histPopupVisible
}

// CurrentQuery returns the query block under the cursor.
func (m Model) CurrentQuery() string {
	return m.currentQuery(m.lines())
}

// WrapCurrentQueryExplain replaces the current query block with a driver-specific EXPLAIN
// (or equivalent) form. Returns false when there is no query under the cursor, the block is
// empty, or the text already looks like an explain request.
func (m *Model) WrapCurrentQueryExplain(driver string) bool {
	lines := m.lines()
	start, end := m.currentQueryBounds(lines)
	if start > end {
		return false
	}
	old := strings.TrimSpace(strings.Join(lines[start:end+1], "\n"))
	newQ, ok := sqlutil.WrapQueryForExplain(driver, old)
	if !ok {
		return false
	}
	m.pushUndoPoint()
	m.vim.mode = ModeNormal
	m.insertUndoSeeded = false
	chunk := strings.Split(newQ, "\n")
	lines = append(lines[:start], append(chunk, lines[end+1:]...)...)
	m.setLines(lines)
	m.vim.row = start
	m.vim.col = 0
	m.compVisible = false
	m.clampCursor()
	m.adjustScroll()
	return true
}

// SetContent replaces the current tab content (e.g. when pressing 's' in explorer).
func (m *Model) SetContent(content string) {
	m.clearTabUndo(tabStoreKey(m.connKey))
	m.setLines(strings.Split(content, "\n"))
	m.vim.row = len(m.lines()) - 1
	m.vim.col = 0
	m.insertUndoSeeded = false
}

// ClearUndoable clears the buffer and leaves the previous text on the undo stack (u / ctrl+r).
func (m *Model) ClearUndoable() {
	if strings.Join(m.lines(), "\n") != "" {
		m.pushUndoPoint()
	}
	m.setLines(strings.Split("", "\n"))
	m.vim.row = len(m.lines()) - 1
	m.vim.col = 0
	m.insertUndoSeeded = false
	m.compVisible = false
	m.clampCursor()
	m.adjustScroll()
}

// AppendAtEnd appends text to the query buffer (blank line before chunk if buffer non-empty).
func (m *Model) AppendAtEnd(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	m.pushUndoPoint()
	m.vim.mode = ModeNormal
	m.insertUndoSeeded = false
	lines := m.lines()
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	chunk := strings.Split(text, "\n")
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, chunk...)
	m.setLines(lines)
	m.vim.row = len(m.lines()) - 1
	m.vim.col = 0
	m.compVisible = false
	m.clampCursor()
	m.adjustScroll()
}

// LastNonBlankLine returns the last non-empty line in the buffer (trimmed), or "".
func (m *Model) LastNonBlankLine() string {
	lines := m.lines()
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}

// AppendInline appends text at the end of the editor on the next line (no blank separator).
func (m *Model) AppendInline(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	m.pushUndoPoint()
	m.vim.mode = ModeNormal
	m.insertUndoSeeded = false
	lines := m.lines()
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	chunk := strings.Split(text, "\n")
	lines = append(lines, chunk...)
	m.setLines(lines)
	m.vim.row = len(m.lines()) - 1
	m.vim.col = 0
	m.compVisible = false
	m.clampCursor()
	m.adjustScroll()
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
			case "tab", "enter":
				// Accept completion
				m.beforeInsertEdit()
				lines = m.acceptCompletion(lines)
				m.setLines(lines)
				m.compVisible = false
				m.clampCursor()
				m.adjustScroll()
				return nil
			case "ctrl+n", "down":
				m.compCursor = (m.compCursor + 1) % len(m.completions)
				return nil
			case "ctrl+p", "up":
				m.compCursor = (m.compCursor - 1 + len(m.completions)) % len(m.completions)
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
			m.insertUndoSeeded = false
			m.compVisible = false
			if m.vim.col > 0 {
				m.vim.col--
			}
			m.setLines(lines)
			m.clampCursor()
			m.adjustScroll()
			return m.queryPanePersistCmd()
		case "backspace":
			m.beforeInsertEdit()
			lines = m.deleteBackspace(lines)
			m.refreshCompletions(lines)
		case "delete":
			m.beforeInsertEdit()
			lines = m.deleteForward(lines)
			m.refreshCompletions(lines)
		case "enter":
			m.beforeInsertEdit()
			lines = m.insertNewline(lines)
			m.compVisible = false
		case "tab":
			// Trigger autocomplete (works even with no typed prefix — suggests by SQL context)
			prefix := m.wordBeforeCursor(lines)
			m.completions = m.completer.CompleteWithContext(prefix, m.beforeCursorText(lines), 8)
			if len(m.completions) > 0 {
				m.compCursor = 0
				m.compVisible = true
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
				m.beforeInsertEdit()
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
				m.beforeInsertEdit()
				lines = m.insertRune(lines, msg.Runes[0])
				m.refreshCompletions(lines)
			}
		}
		m.setLines(lines)
		m.clampCursor()
		m.adjustScroll()
		return nil
	}

	// Normal mode — history popup: ↑/↓ navigate list; typing filters; backspace edits filter or closes
	if m.histPopupVisible && len(m.history) > 0 {
		if m.consumeHistoryPopupNav(msg) {
			return nil
		}
		if m.histPopupPendingDeleteQuery == "" {
			if m.consumeHistoryPopupFilter(msg) {
				return nil
			}
		}
	}

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
		m.pushUndoPoint()
		lines = m.openLineBelow(lines)
		m.setLines(lines)
		m.vim.mode = ModeInsert
		m.insertUndoSeeded = true // checkpoint is the push before open-line (whole O/o session)
	case "O":
		m.pushUndoPoint()
		lines = m.openLineAbove(lines)
		m.setLines(lines)
		m.vim.mode = ModeInsert
		m.insertUndoSeeded = true
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
	case "J":
		if nextRow := m.jumpToNextQuery(lines); nextRow >= 0 {
			m.vim.row = nextRow
			m.vim.col = 0
			m.clampCursor()
			m.adjustScroll()
		}
	case "K":
		if prevRow := m.jumpToPrevQuery(lines); prevRow >= 0 {
			m.vim.row = prevRow
			m.vim.col = 0
			m.clampCursor()
			m.adjustScroll()
		}
	case "tab":
		if msg := m.CycleTab(1); msg != nil {
			return func() tea.Msg { return *msg }
		}
	case "shift+tab", "backtab":
		if msg := m.CycleTab(-1); msg != nil {
			return func() tea.Msg { return *msg }
		}
	case "0":
		if m.vim.pendingD {
			m.pushUndoPoint()
			lines = m.deleteToLineStart(lines)
			m.setLines(lines)
			m.vim.pendingD = false
		} else if m.vim.pendingY {
			m.yankToLineStart(lines)
			m.vim.pendingY = false
		} else {
			m.vim.col = 0
		}
	case "$":
		if m.vim.pendingD {
			m.pushUndoPoint()
			lines = m.deleteToLineEnd(lines)
			m.setLines(lines)
			m.vim.pendingD = false
		} else if m.vim.pendingY {
			m.yankToLineEnd(lines)
			m.vim.pendingY = false
		} else if len(lines) > 0 {
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
		if m.vim.pendingD {
			m.pushUndoPoint()
			lines = m.deleteWordForward(lines)
			m.setLines(lines)
			m.vim.pendingD = false
		} else if m.vim.pendingY {
			m.yankWordForward(lines)
			m.vim.pendingY = false
		} else {
			m.vim.row, m.vim.col = wordForward(lines, m.vim.row, m.vim.col)
		}
	case "b":
		m.vim.row, m.vim.col = wordBackward(lines, m.vim.row, m.vim.col)
	case "x":
		m.pushUndoPoint()
		lines = m.deleteCharAt(lines)
		m.setLines(lines)
	case "c":
		if m.vim.pendingC {
			m.pushUndoPoint()
			lines = m.cleanLine(lines)
			m.setLines(lines)
			m.vim.pendingC = false
		} else {
			m.vim.pendingC = true
		}
	case "d":
		if m.histPopupVisible && m.histPopupPendingDeleteQuery != "" {
			m.histPopupPendingDeleteQuery = ""
			m.vim.pendingD = false
			return nil
		}
		if m.vim.pendingD {
			m.pushUndoPoint()
			lines = m.deleteLine(lines)
			m.setLines(lines)
			m.vim.pendingD = false
		} else {
			m.vim.pendingD = true
		}
	case "q":
		if m.vim.pendingD {
			m.pushUndoPoint()
			lines = m.deleteQuery(lines)
			m.setLines(lines)
			m.vim.pendingD = false
		} else if m.vim.pendingY {
			q := m.currentQuery(lines)
			if q != "" {
				util.Copy(q)
			}
			m.vim.pendingY = false
		} else if m.vim.pendingC {
			m.pushUndoPoint()
			lines = m.cleanQuery(lines)
			m.setLines(lines)
			m.vim.pendingC = false
		}
	case "ctrl+d":
		if m.histPopupVisible && m.histPopupPendingDeleteQuery != "" {
			return nil
		}
		if m.histPopupVisible && len(m.histPopupFilteredIdx) > 0 && m.histPopupCursor < len(m.histPopupFilteredIdx) {
			m.histPopupPendingDeleteQuery = m.history[m.histPopupFilteredIdx[m.histPopupCursor]]
			m.vim.pendingD = false
			return nil
		}
	case "y":
		if m.histPopupVisible && m.histPopupPendingDeleteQuery != "" {
			q := m.histPopupPendingDeleteQuery
			m.histPopupPendingDeleteQuery = ""
			return func() tea.Msg { return DeleteHistoryEntryMsg{Query: q} }
		}
		if m.vim.pendingY {
			if len(lines) > 0 && m.vim.row < len(lines) {
				util.Copy(lines[m.vim.row])
			}
			m.vim.pendingY = false
		} else {
			m.vim.pendingY = true
		}
	case "n":
		if m.histPopupVisible {
			m.histPopupPendingDeleteQuery = ""
			return nil
		}
	case "u":
		if m.histPopupVisible {
			return nil
		}
		if m.Undo() {
			return m.queryPanePersistCmd()
		}
		return nil
	case "ctrl+r":
		if m.histPopupVisible {
			return nil
		}
		if m.Redo() {
			return m.queryPanePersistCmd()
		}
		return nil
	case "enter", "ctrl+enter", "f5":
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
		if !m.histPopupVisible && len(m.history) > 0 {
			m.histPopupVisible = true
			m.histPopupCursor = 0
			m.histPopupPendingDeleteQuery = ""
			m.histPopupFilter = ""
			m.rebuildHistoryPopupFilter()
		}
		return nil
	case "esc":
		if m.histPopupVisible {
			if m.histPopupPendingDeleteQuery != "" {
				m.histPopupPendingDeleteQuery = ""
			} else {
				m.closeHistoryPopup()
			}
			return nil
		}
		m.vim.pendingG = false
		m.vim.pendingD = false
		m.vim.pendingY = false
		m.vim.pendingC = false
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
		m.vim.pendingY = false
		m.vim.pendingC = false
	}

	m.clampCursor()
	m.adjustScroll()
	return nil
}

// consumeHistoryPopupNav handles arrow up/down and ctrl+p/n for the history popup list only.
// Returns true if the key was consumed and vim motion must not run.
func (m *Model) consumeHistoryPopupNav(msg tea.KeyMsg) bool {
	if !m.histPopupVisible || len(m.history) == 0 {
		return false
	}
	if m.histPopupPendingDeleteQuery != "" {
		// Delete confirm: swallow arrows so they don't move the cursor in the buffer
		switch msg.String() {
		case "down", "up", "ctrl+n", "ctrl+p":
			return true
		}
		return false
	}
	n := len(m.histPopupFilteredIdx)
	down := false
	up := false
	switch msg.String() {
	case "down", "ctrl+n":
		down = true
	case "up", "ctrl+p":
		up = true
	}
	if down {
		if n > 0 && m.histPopupCursor < n-1 {
			m.histPopupCursor++
		}
		return true
	}
	if up {
		if n > 0 && m.histPopupCursor > 0 {
			m.histPopupCursor--
		}
		return true
	}
	return false
}

// consumeHistoryPopupFilter handles backspace (edit filter or close) and typed filter text.
func (m *Model) consumeHistoryPopupFilter(msg tea.KeyMsg) bool {
	if !m.histPopupVisible || len(m.history) == 0 || m.histPopupPendingDeleteQuery != "" {
		return false
	}
	switch msg.String() {
	case "backspace":
		if m.histPopupFilter == "" {
			m.closeHistoryPopup()
		} else {
			r := []rune(m.histPopupFilter)
			m.histPopupFilter = string(r[:len(r)-1])
			m.rebuildHistoryPopupFilter()
			m.histPopupCursor = 0
		}
		return true
	}
	// Let navigation keys fall through (handled before this function is called).
	switch msg.String() {
	case "up", "down", "ctrl+p", "ctrl+n", "enter", "esc", "tab", "shift+tab", "backtab":
		return false
	}
	// Space is KeySpace, not KeyRunes, in bubbletea.
	if msg.Type == tea.KeySpace {
		m.histPopupFilter += " "
		m.rebuildHistoryPopupFilter()
		m.histPopupCursor = 0
		return true
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		r := msg.Runes[0]
		if unicode.IsPrint(r) {
			m.histPopupFilter += string(r)
			m.rebuildHistoryPopupFilter()
			m.histPopupCursor = 0
			return true
		}
	}
	return false
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

// lineVisualForWrap returns the string passed to the wrapper for logical line i, matching View
// (syntax-highlighted, or plain with reversed cursor cell when focused on that line).
func (m Model) lineVisualForWrap(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return ""
	}
	lineStr := lines[i]
	lineRunes := []rune(lineStr)
	rendered := renderHighlighted(lineStr, m.theme)
	if m.focused && i == m.vim.row {
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
	return rendered
}

// wrappedRowsForVisual returns display lines for one logical row after wrapping (same algorithm as lipgloss Width).
func wrappedRowsForVisual(visual string, width int) []string {
	if width < 1 {
		width = 1
	}
	if visual == "" {
		return []string{""}
	}
	out := cellbuf.Wrap(visual, width, "")
	if out == "" {
		return []string{""}
	}
	return strings.Split(out, "\n")
}

func (m Model) wrappedRowCount(lines []string, i, width int) int {
	return len(wrappedRowsForVisual(m.lineVisualForWrap(lines, i), width))
}

// cursorSentinel marks the cursor column when mapping to wrapped rows (avoids ambiguous strings.Index).
const cursorSentinel = "\ufffc"

func (m Model) cursorSubRowInWrappedLine(lines []string, lineIdx, width int) int {
	if !m.focused || lineIdx != m.vim.row || lineIdx < 0 || lineIdx >= len(lines) {
		return 0
	}
	lineStr := lines[lineIdx]
	lineRunes := []rune(lineStr)
	col := m.vim.col
	if col > len(lineRunes) {
		col = len(lineRunes)
	}
	before := string(lineRunes[:col])
	after := ""
	if col < len(lineRunes) {
		after = string(lineRunes[col+1:])
	}
	probe := before + cursorSentinel + after
	wrapped := cellbuf.Wrap(probe, width, "")
	idx := strings.Index(wrapped, cursorSentinel)
	if idx < 0 {
		return 0
	}
	return strings.Count(wrapped[:idx], "\n")
}

func (m Model) globalCursorDisplayRow(lines []string, width int) int {
	if len(lines) == 0 {
		return 0
	}
	row := m.vim.row
	if row >= len(lines) {
		row = len(lines) - 1
	}
	total := 0
	for i := 0; i < row; i++ {
		total += m.wrappedRowCount(lines, i, width)
	}
	total += m.cursorSubRowInWrappedLine(lines, row, width)
	return total
}

func (m Model) totalWrappedDisplayRows(lines []string, width int) int {
	if len(lines) == 0 {
		return 1
	}
	n := 0
	for i := 0; i < len(lines); i++ {
		n += m.wrappedRowCount(lines, i, width)
	}
	if n < 1 {
		return 1
	}
	return n
}

func (m *Model) adjustScroll() {
	viewportRows := m.height - 3 - editorTopGutterLines
	if viewportRows < 1 {
		viewportRows = 1
	}
	width := m.width
	if width < 1 {
		width = 1
	}
	lines := m.lines()

	cursorY := m.globalCursorDisplayRow(lines, width)
	total := m.totalWrappedDisplayRows(lines, width)
	maxScroll := max(0, total-viewportRows)
	if m.scrollTop > maxScroll {
		m.scrollTop = maxScroll
	}

	if cursorY < m.scrollTop {
		m.scrollTop = cursorY
	}
	if cursorY >= m.scrollTop+viewportRows {
		m.scrollTop = cursorY - viewportRows + 1
	}
	if m.scrollTop > maxScroll {
		m.scrollTop = maxScroll
	}
	if m.scrollTop < 0 {
		m.scrollTop = 0
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

func (m *Model) deleteForward(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	row, col := m.vim.row, m.vim.col
	runes := []rune(lines[row])
	if col < len(runes) {
		runes = append(runes[:col], runes[col+1:]...)
		lines[row] = string(runes)
		return lines
	}
	if row < len(lines)-1 {
		lines[row] = lines[row] + lines[row+1]
		lines = append(lines[:row+1], lines[row+2:]...)
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

func (m *Model) cleanLine(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	row := m.vim.row
	lines[row] = cleanLineText(lines[row])
	return lines
}

func (m *Model) cleanQuery(lines []string) []string {
	start, end := m.currentQueryBounds(lines)
	if start > end {
		return lines
	}
	for i := start; i <= end; i++ {
		lines[i] = cleanLineText(lines[i])
	}
	return lines
}

func cleanLineText(line string) string {
	loc := lineNumRegex.FindStringIndex(line)
	if loc == nil {
		return line
	}
	trimmed := line[loc[1]:]
	if len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t') {
		trimmed = trimmed[1:]
	}
	return trimmed
}

// currentWordBounds returns [start, end) rune indices of the word at or next to col,
// using the same alphanumeric+underscore rule as autocomplete (isWordRune).
func currentWordBounds(line string, col int) (start, end int, ok bool) {
	runes := []rune(line)
	if col < 0 {
		col = 0
	}
	if col > len(runes) {
		col = len(runes)
	}
	if len(runes) == 0 {
		return 0, 0, false
	}
	if col < len(runes) && isWordRune(runes[col]) {
		start = col
		for start > 0 && isWordRune(runes[start-1]) {
			start--
		}
		end = col + 1
		for end < len(runes) && isWordRune(runes[end]) {
			end++
		}
		return start, end, true
	}
	// On whitespace / punctuation: prefer the next word to the right (e.g. space between SELECT and foo).
	for c := col; c < len(runes); c++ {
		if isWordRune(runes[c]) {
			start = c
			end = c + 1
			for end < len(runes) && isWordRune(runes[end]) {
				end++
			}
			return start, end, true
		}
	}
	if col > 0 && isWordRune(runes[col-1]) {
		end = col
		start = col - 1
		for start > 0 && isWordRune(runes[start-1]) {
			start--
		}
		return start, end, true
	}
	return 0, 0, false
}

func (m *Model) deleteWordForward(lines []string) []string {
	if len(lines) == 0 || m.vim.row >= len(lines) {
		return lines
	}
	row := m.vim.row
	line := lines[row]
	start, end, ok := currentWordBounds(line, m.vim.col)
	if !ok || end <= start {
		return lines
	}
	runes := []rune(line)
	lines[row] = string(append(append([]rune{}, runes[:start]...), runes[end:]...))
	m.vim.col = start
	return lines
}

func (m *Model) deleteToLineEnd(lines []string) []string {
	if len(lines) == 0 || m.vim.row >= len(lines) {
		return lines
	}
	runes := []rune(lines[m.vim.row])
	if m.vim.col >= len(runes) {
		return lines
	}
	lines[m.vim.row] = string(runes[:m.vim.col])
	return lines
}

// deleteToLineStart removes text from column 0 up to (but not including) the cursor.
func (m *Model) deleteToLineStart(lines []string) []string {
	if len(lines) == 0 || m.vim.row >= len(lines) {
		return lines
	}
	runes := []rune(lines[m.vim.row])
	col := m.vim.col
	if col > len(runes) {
		col = len(runes)
	}
	lines[m.vim.row] = string(runes[col:])
	m.vim.col = 0
	return lines
}

func (m *Model) yankWordForward(lines []string) {
	if len(lines) == 0 || m.vim.row >= len(lines) {
		return
	}
	line := lines[m.vim.row]
	start, end, ok := currentWordBounds(line, m.vim.col)
	if !ok || end <= start {
		return
	}
	runes := []rune(line)
	_ = util.Copy(string(runes[start:end]))
}

func (m *Model) yankToLineEnd(lines []string) {
	if len(lines) == 0 || m.vim.row >= len(lines) {
		return
	}
	runes := []rune(lines[m.vim.row])
	if m.vim.col >= len(runes) {
		return
	}
	_ = util.Copy(string(runes[m.vim.col:]))
}

func (m *Model) yankToLineStart(lines []string) {
	if len(lines) == 0 || m.vim.row >= len(lines) {
		return
	}
	runes := []rune(lines[m.vim.row])
	col := m.vim.col
	if col > len(runes) {
		col = len(runes)
	}
	if col <= 0 {
		return
	}
	_ = util.Copy(string(runes[:col]))
}

func (m *Model) refreshCompletions(lines []string) {
	prefix := m.wordBeforeCursor(lines)
	before := m.beforeCursorText(lines)

	if m.compVisible {
		// Popup already open: keep list in sync (typing filters; empty prefix → context “top picks” again)
		m.completions = m.completer.CompleteWithContext(prefix, before, 8)
		m.compCursor = 0
		if len(m.completions) == 0 {
			m.compVisible = false
		}
		return
	}

	// Popup closed: auto-open while typing a non-empty identifier prefix (previous behavior)
	if prefix == "" {
		return
	}
	m.completions = m.completer.CompleteWithContext(prefix, before, 8)
	m.compCursor = 0
	m.compVisible = len(m.completions) > 0
}

func (m *Model) beforeCursorText(lines []string) string {
	if len(lines) == 0 || m.vim.row >= len(lines) {
		return ""
	}
	runes := []rune(lines[m.vim.row])
	col := m.vim.col
	if col > len(runes) {
		col = len(runes)
	}
	return string(runes[:col])
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
	newRunes := make([]rune, 0, len(runes)-(end-start)+len(completion))
	newRunes = append(newRunes, runes[:start]...)
	newRunes = append(newRunes, completion...)
	newRunes = append(newRunes, runes[end:]...)
	lines[m.vim.row] = string(newRunes)
	m.vim.col = start + len(completion)
	return lines
}

// currentQueryBounds returns the start and end row indices of the query block the cursor is in.
// Returns start > end if no block is found.
func (m *Model) currentQueryBounds(lines []string) (int, int) {
	if len(lines) == 0 {
		return 0, -1
	}
	row := m.vim.row
	if row >= len(lines) {
		row = len(lines) - 1
	}

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
		return start, end
	}

	// Cursor on blank line: find the block above
	if row > 0 {
		endAbove := row - 1
		for endAbove >= 0 && strings.TrimSpace(lines[endAbove]) == "" {
			endAbove--
		}
		if endAbove >= 0 {
			startAbove := endAbove
			for startAbove > 0 && strings.TrimSpace(lines[startAbove-1]) != "" {
				startAbove--
			}
			return startAbove, endAbove
		}
	}
	return 0, -1
}

// currentQuery returns the query block that the cursor is currently in.
// Queries are separated by at least one blank line.
// If the cursor is on a blank line, the block above (if any) is used.
func (m *Model) currentQuery(lines []string) string {
	start, end := m.currentQueryBounds(lines)
	if start <= end {
		return strings.TrimSpace(strings.Join(lines[start:end+1], "\n"))
	}
	return ""
}

// MoveCursorToQueryBlockIfPresent moves the cursor to the start of the first query block
// whose trimmed text equals target (after trim). Query blocks are separated by blank lines,
// matching CurrentQuery boundaries. Returns true if a matching block was found.
func (m *Model) MoveCursorToQueryBlockIfPresent(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	lines := m.lines()
	i := 0
	for i < len(lines) {
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		if i >= len(lines) {
			break
		}
		start := i
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			i++
		}
		end := i - 1
		q := strings.TrimSpace(strings.Join(lines[start:end+1], "\n"))
		if q == target {
			m.vim.mode = ModeNormal
			m.insertUndoSeeded = false
			m.vim.row = start
			m.vim.col = 0
			m.compVisible = false
			m.clampCursor()
			m.adjustScroll()
			return true
		}
	}
	return false
}

// deleteQuery deletes the current query block and returns the updated lines.
// If the next line after the block is blank (query separator), it is removed too
// so two queries do not end up separated by multiple empty lines.
func (m *Model) deleteQuery(lines []string) []string {
	start, end := m.currentQueryBounds(lines)
	if start > end {
		return lines
	}
	delEnd := end
	if delEnd+1 < len(lines) && strings.TrimSpace(lines[delEnd+1]) == "" {
		delEnd++
	}
	lines = append(lines[:start], lines[delEnd+1:]...)
	if len(lines) == 0 {
		lines = []string{""}
	}
	m.vim.row = start
	if m.vim.row >= len(lines) {
		m.vim.row = len(lines) - 1
	}
	if m.vim.row < 0 {
		m.vim.row = 0
	}
	return lines
}

func (m *Model) jumpToNextQuery(lines []string) int {
	if len(lines) == 0 {
		return -1
	}
	row := m.vim.row
	if row >= len(lines) {
		row = len(lines) - 1
	}
	start := row
	for start < len(lines) && strings.TrimSpace(lines[start]) != "" {
		start++
	}
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) {
		return -1
	}
	return start
}

func (m *Model) jumpToPrevQuery(lines []string) int {
	if len(lines) == 0 {
		return -1
	}
	row := m.vim.row
	if row >= len(lines) {
		row = len(lines) - 1
	}
	end := row
	for end > 0 && strings.TrimSpace(lines[end]) != "" {
		end--
	}
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if end <= 0 {
		return 0
	}
	start := end
	for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	if start == end {
		return 0
	}
	return start
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	lines := m.lines()
	// Tab row + gutter + editor rows: same total inner height as before gutter (height-2 content rows under the pane chrome).
	viewportRows := m.height - 3 - editorTopGutterLines
	if viewportRows < 1 {
		viewportRows = 1
	}
	width := m.width
	if width < 1 {
		width = 1
	}

	var sb strings.Builder

	modeLabel := " " + m.vim.mode.String() + " "
	sb.WriteString(renderTabBar(m.theme, m.openTabs, m.activeTabIdx, modeLabel, m.width) + "\n")
	for i := 0; i < editorTopGutterLines; i++ {
		sb.WriteString(lipgloss.NewStyle().Width(m.width).Render("") + "\n")
	}

	// Render visible wrapped rows; scrollTop is a display-row offset into the wrapped document.
	remainingSkip := m.scrollTop
	emitted := 0
	for i := 0; i < len(lines) && emitted < viewportRows; i++ {
		vis := m.lineVisualForWrap(lines, i)
		for _, p := range wrappedRowsForVisual(vis, width) {
			if remainingSkip > 0 {
				remainingSkip--
				continue
			}
			sb.WriteString(lipgloss.NewStyle().Width(width).Render(p) + "\n")
			emitted++
			if emitted >= viewportRows {
				break
			}
		}
	}
	for emitted < viewportRows {
		sb.WriteString(lipgloss.NewStyle().Width(width).Render("") + "\n")
		emitted++
	}

	result := sb.String()

	// History popup — centered overlay; query buffer stays visible underneath
	if m.histPopupVisible && len(m.history) > 0 {
		return m.overlayHistoryPopup(result, viewportRows)
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

		// Merge with overlayStyledBlockAt so ANSI on the line below the cursor is not split.
		// Vertically: prefer below the cursor; if the box would extend past the pane, flip above;
		// else pin to the bottom content row (overlayStyledBlockAt drops rows past the last line).
		cursorDisplayRow := m.globalCursorDisplayRow(lines, width)
		cursorScreenRow := 1 + editorTopGutterLines + (cursorDisplayRow - m.scrollTop)
		if cursorScreenRow < 1+editorTopGutterLines {
			cursorScreenRow = 1 + editorTopGutterLines
		}
		baseLines := strings.Split(result, "\n")
		boxH := lipgloss.Height(compBox)
		insertRow := completionPopupStartRow(cursorScreenRow, boxH, len(baseLines))
		insertCol := m.vim.col + 2
		result = overlayStyledBlockAt(result, compBox, insertRow, insertCol, m.width)
	}

	// Render pending operator popup
	if m.vim.pendingD || m.vim.pendingY || m.vim.pendingC {
		title := "Delete"
		cmdKey := "d"
		actionDesc := "line"
		if m.vim.pendingY {
			title = "Yank"
			cmdKey = "y"
		} else if m.vim.pendingC {
			title = "Clean"
			cmdKey = "c"
			actionDesc = "line"
		}
		
		var rows []struct{ key, desc string }
		if m.vim.pendingC {
			rows = []struct{ key, desc string }{
				{cmdKey, "Current " + actionDesc},
				{"q", "Current query"},
			}
		} else {
			rows = []struct{ key, desc string }{
				{cmdKey, "Current " + actionDesc},
				{"q", "Current query"},
				{"w", "Current word"},
				{"$", "To end of line"},
				{"0", "To start of line"},
			}
		}
		gap1 := m.theme.PaletteFill.Render(" ")
		gap2 := m.theme.PaletteFill.Render("  ")
		innerW := lipgloss.Width(m.theme.PaletteTitle.Render(title))
		for _, r := range rows {
			kPlain := m.theme.PaletteKey.Render(r.key)
			dPlain := m.theme.PaletteItem.Render(r.desc)
			lineW := lipgloss.Width(gap1 + kPlain + gap2 + dPlain)
			if lineW > innerW {
				innerW = lineW
			}
		}
		if innerW < 12 {
			innerW = 12
		}

		rowStyler := m.theme.PaletteFill.Copy().Width(innerW).Align(lipgloss.Left)
		var opSb strings.Builder
		opSb.WriteString(rowStyler.Render(m.theme.PaletteTitle.Render(title)) + "\n")
		for _, r := range rows {
			k := m.theme.PaletteKey.Render(r.key)
			d := m.theme.PaletteItem.Render(r.desc)
			opSb.WriteString(rowStyler.Render(gap1+k+gap2+d) + "\n")
		}
		
		box := m.theme.PaletteBox.Render(opSb.String())
		
		boxH := lipgloss.Height(box)
		boxW := lipgloss.Width(box)
		
		totalLines := 1 + editorTopGutterLines + viewportRows
		startRow := totalLines - boxH
		if startRow < 0 { startRow = 0 }
		startCol := m.width - boxW - 2
		if startCol < 0 { startCol = 0 }
		
		result = overlayStyledBlockAt(result, box, startRow, startCol, m.width)
	}

	return result
}

// overlayHistoryPopup draws the history list or delete-confirm box centered over the editor content.
func (m Model) overlayHistoryPopup(base string, visibleLines int) string {
	box := m.buildHistoryPopupBox(visibleLines)
	if box == "" {
		return base
	}
	boxLines := strings.Split(box, "\n")
	boxH := len(boxLines)
	boxW := 0
	for _, l := range boxLines {
		if w := lipgloss.Width(l); w > boxW {
			boxW = w
		}
	}
	if boxH < 1 || boxW < 1 {
		return base
	}
	startRow := 1 + editorTopGutterLines
	if visibleLines > boxH {
		startRow += (visibleLines - boxH) / 2
	}
	startCol := 0
	if m.width > boxW {
		startCol = (m.width - boxW) / 2
	}
	if startCol+boxW > m.width {
		startCol = max(0, m.width-boxW)
	}
	return overlayStyledBlockAt(base, box, startRow, startCol, m.width)
}

func (m Model) buildHistoryPopupBox(visibleLines int) string {
	innerW := m.width - 8
	if innerW < 20 {
		innerW = 20
	}
	if innerW > m.width-4 {
		innerW = m.width - 4
	}

	if m.histPopupPendingDeleteQuery != "" {
		preview := strings.ReplaceAll(m.histPopupPendingDeleteQuery, "\n", " ↵ ")
		textW := innerW - 4
		if textW < 8 {
			textW = 8
		}
		previewLines := wrapRunesToWidth([]rune(preview), textW)
		maxPreview := visibleLines - 6
		if maxPreview < 2 {
			maxPreview = 2
		}
		if len(previewLines) > maxPreview {
			previewLines = previewLines[:maxPreview]
			previewLines = append(previewLines, "...")
		}
		hint := m.theme.Dimmed.Render("Remove from history — y: confirm · Esc/n: cancel")
		inner := lipgloss.NewStyle().Bold(true).Render("Delete this query?") + "\n\n" +
			strings.Join(previewLines, "\n") + "\n\n" + hint
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("9")).
			Width(innerW).
			Padding(0, 1).
			Render(inner)
	}

	maxListRows := visibleLines - 4
	if maxListRows < 1 {
		maxListRows = 1
	}

	fr := m.histPopupFilteredIdx
	n := len(fr)
	start := 0
	if n > 0 && m.histPopupCursor >= maxListRows {
		start = m.histPopupCursor - maxListRows + 1
	}
	end := start + maxListRows
	if end > n {
		end = n
	}

	var b strings.Builder
	if m.histPopupFilter != "" {
		q := m.histPopupFilter
		if len([]rune(q)) > 36 {
			rq := []rune(q)
			q = string(rq[:33]) + "..."
		}
		b.WriteString(m.theme.Dimmed.Render(fmt.Sprintf(" %d matches · %q", n, q)))
	} else {
		b.WriteString(m.theme.Dimmed.Render(fmt.Sprintf(" History (%d)", n)))
	}
	b.WriteString("\n")

	textW := innerW - 6
	if textW < 8 {
		textW = 8
	}
	if n == 0 {
		b.WriteString(" " + m.theme.Dimmed.Render("No matching queries"))
		b.WriteString("\n")
	} else {
		for i := start; i < end; i++ {
			histIdx := fr[i]
			preview := strings.ReplaceAll(m.history[histIdx], "\n", " ↵ ")
			runes := []rune(preview)
			if len(runes) > textW {
				preview = string(runes[:textW-3]) + "..."
			}
			line := " " + preview
			if i == m.histPopupCursor {
				b.WriteString(m.theme.TreeSelected.Width(innerW - 2).Render(line))
			} else {
				b.WriteString(lipgloss.NewStyle().Width(innerW - 2).Render(line))
			}
			b.WriteString("\n")
		}
	}
	inner := strings.TrimSuffix(b.String(), "\n")
	borderFG := lipgloss.Color("12")
	main := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderFG).
		Width(innerW).
		Padding(0, 1).
		BorderBottom(false).
		Render(inner)
	lines := strings.Split(main, "\n")
	if len(lines) == 0 {
		return main
	}
	w := lipgloss.Width(lines[0])
	hint := m.theme.Dimmed.Render(" ↑↓: navigate · Enter: insert · Esc: close · type to filter · Ctrl+d: delete ")
	bottom := renderHistoryPopupBottomBorder(w, hint, borderFG)
	return strings.Join(lines, "\n") + "\n" + bottom
}

// renderHistoryPopupBottomBorder draws the rounded bottom edge with a right-aligned hint.
func renderHistoryPopupBottomBorder(totalW int, hintStyled string, borderFG lipgloss.TerminalColor) string {
	if totalW < 3 {
		return ""
	}
	hw := ansi.StringWidth(hintStyled)
	mid := totalW - 2 // between ╰ and ╯
	if hw > mid {
		hintStyled = ansi.Truncate(hintStyled, mid, "")
		hw = ansi.StringWidth(hintStyled)
	}
	dashN := mid - hw
	if dashN < 0 {
		dashN = 0
	}
	edge := lipgloss.NewStyle().Foreground(borderFG)
	return edge.Render("╰") + edge.Render(strings.Repeat("─", dashN)) + hintStyled + edge.Render("╯")
}

// completionPopupStartRow returns the top row index for the autocomplete box so it stays
// inside the rendered editor string. Prefer below the cursor; if that overflows the pane,
// place above (insertRow >= 1 so the tab bar row is not covered); else pin to the bottom.
// Placement uses completionPopupEdgeMargin so we do not use trailing buffer rows (split
// artifact) or the last row without enough space for the full rounded border.
func completionPopupStartRow(cursorScreenRow, boxH, totalRows int) int {
	m := completionPopupEdgeMargin
	if boxH <= 0 {
		return cursorScreenRow + 1
	}
	// Last row index the popup may occupy (reserve bottom margin).
	last := totalRows - 1 - m
	if last < 1 {
		last = 1
	}
	belowStart := cursorScreenRow + 1
	belowEnd := belowStart + boxH - 1
	if belowEnd <= last {
		return belowStart
	}
	// Above cursor: last popup row is cursorScreenRow-1.
	aboveStart := cursorScreenRow - boxH
	if aboveStart >= 1 && cursorScreenRow-1 <= last {
		return aboveStart
	}
	insertRow := last - boxH + 1
	if insertRow < 1 {
		insertRow = 1
	}
	return insertRow
}

// overlayStyledBlockAt merges a lipgloss-styled overlay onto base using display-cell widths
// (ANSI-safe). Rune-by-rune overlay breaks escape sequences and clips rounded borders.
func overlayStyledBlockAt(base string, overlay string, startRow, startCol int, width int) string {
	baseLines := strings.Split(base, "\n")
	overLines := strings.Split(overlay, "\n")
	for i, ol := range overLines {
		row := startRow + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		line := baseLines[row]
		ow := ansi.StringWidth(ol)
		if ow == 0 {
			continue
		}
		nc := startCol
		if nc < 0 {
			nc = 0
		}
		if nc >= width {
			continue
		}
		if nc+ow > width {
			ol = ansi.Truncate(ol, width-nc, "")
			ow = ansi.StringWidth(ol)
			if ow == 0 {
				continue
			}
		}
		lineW := ansi.StringWidth(line)
		left := ansi.Cut(line, 0, nc)
		right := ansi.Cut(line, nc+ow, lineW)
		merged := left + ol + right
		baseLines[row] = lipgloss.NewStyle().Width(width).Render(merged)
	}
	return strings.Join(baseLines, "\n")
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
