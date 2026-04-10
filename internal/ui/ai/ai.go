package ai

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	internalAi "github.com/robertn/dbx/internal/ai"
	"github.com/robertn/dbx/internal/ui/theme"
)

type Mode int

const (
	ModeInput Mode = iota
	ModeOutput
)

// AIResponseMsg is sent back when the CLI AI tool responds.
type AIResponseMsg struct {
	Response string
	Err      error
}

// AISendPromptMsg is sent when the user submits a prompt so app.go can inject DDL context.
type AISendPromptMsg struct {
	ConnKey string
	Prompt  string
}

// ExtractSQLMsg is sent when user hits enter in output mode and a SQL block is found.
type ExtractSQLMsg struct {
	SQL string
}

func AskCmd(store *internalAi.Store, connKey, prompt string) tea.Cmd {
	return func() tea.Msg {
		resp, err := store.Ask(connKey, prompt)
		return AIResponseMsg{
			Response: resp,
			Err:      err,
		}
	}
}

// Model is the bubbletea model for the AI assistant pane.
type Model struct {
	theme   theme.Theme
	width   int
	height  int
	focused bool

	mode    Mode
	connKey string
	Store   *internalAi.Store

	textarea textarea.Model

	// Output transcript (split lines, same as former viewport content).
	outputLines   []string
	outputScrollY int
	outputScrollX int
	outCursorLine int
	outCursorCol  int

	loading bool

	// SQL availability for current session
	hasSQL bool

	// Dropdown state for @ or #
	showOverlay     bool
	overlayType     rune // '@' or '#'
	overlayAllItems []string
	overlayFiltered []string
	overlayCursor   int
	overlayQuery    string
}

const inputH = 3
const statusH = 1

// New creates a new AI model.
func New(t theme.Theme, store *internalAi.Store) Model {
	ta := textarea.New()
	ta.Placeholder = "Ask the AI assistant... (@ for tables, # for columns, enter to send)"
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(false)

	return Model{
		theme:    t,
		mode:     ModeOutput,
		Store:    store,
		textarea: ta,
	}
}

func (m *Model) SetTheme(t theme.Theme) {
	m.theme = t
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.recalcSizes()
	// Re-render wrapped content at new width
	m.refreshViewport()
}

func (m *Model) recalcSizes() {
	m.textarea.SetWidth(m.width)
	m.textarea.SetHeight(inputH)
}

func (m Model) outputViewHeight() int {
	h := m.height - statusH - inputH - 1
	if h < 0 {
		return 0
	}
	return h
}

func (m Model) maxOutputScrollY() int {
	h := m.outputViewHeight()
	n := len(m.outputLines)
	if h <= 0 || n == 0 {
		return 0
	}
	return max(0, n-h)
}

func clampInt(v, low, high int) int {
	if high < low {
		low, high = high, low
	}
	return min(high, max(low, v))
}

func (m Model) maxColOnLine(lineIdx int) int {
	if lineIdx < 0 || lineIdx >= len(m.outputLines) {
		return 0
	}
	w := ansi.StringWidth(m.outputLines[lineIdx])
	if w == 0 {
		return 0
	}
	return w - 1
}

func (m *Model) clampOutCursorToBuffer() {
	if len(m.outputLines) == 0 {
		m.outCursorLine = 0
		m.outCursorCol = 0
		return
	}
	m.outCursorLine = clampInt(m.outCursorLine, 0, len(m.outputLines)-1)
	m.outCursorCol = clampInt(m.outCursorCol, 0, m.maxColOnLine(m.outCursorLine))
}

func (m *Model) scrollOutputToShowCursor() {
	h := m.outputViewHeight()
	if h <= 0 || len(m.outputLines) == 0 {
		return
	}
	top := m.outputScrollY
	if m.outCursorLine < top {
		m.outputScrollY = m.outCursorLine
	} else if m.outCursorLine >= top+h {
		m.outputScrollY = m.outCursorLine - h + 1
	}
	m.outputScrollY = clampInt(m.outputScrollY, 0, m.maxOutputScrollY())

	line := m.outputLines[m.outCursorLine]
	lw := ansi.StringWidth(line)
	w := m.width
	if lw <= w {
		m.outputScrollX = 0
		return
	}
	c := m.outCursorCol
	if c < m.outputScrollX {
		m.outputScrollX = c
	}
	if c >= m.outputScrollX+w {
		m.outputScrollX = c - w + 1
	}
	maxX := max(0, lw-w)
	m.outputScrollX = clampInt(m.outputScrollX, 0, maxX)
}

func (m *Model) moveOutCursorLine(delta int) {
	if len(m.outputLines) == 0 {
		return
	}
	m.outCursorLine = clampInt(m.outCursorLine+delta, 0, len(m.outputLines)-1)
	m.outCursorCol = min(m.outCursorCol, m.maxColOnLine(m.outCursorLine))
	m.scrollOutputToShowCursor()
}

func (m *Model) moveOutCursorCol(delta int) {
	if len(m.outputLines) == 0 {
		return
	}
	m.clampOutCursorToBuffer()
	mc := m.maxColOnLine(m.outCursorLine)
	m.outCursorCol = clampInt(m.outCursorCol+delta, 0, mc)
	m.scrollOutputToShowCursor()
}

func (m *Model) scrollOutputWindow(delta int) {
	if len(m.outputLines) == 0 {
		return
	}
	h := m.outputViewHeight()
	m.outputScrollY = clampInt(m.outputScrollY+delta, 0, m.maxOutputScrollY())
	m.outCursorLine = clampInt(m.outCursorLine+delta, 0, len(m.outputLines)-1)
	m.outCursorCol = min(m.outCursorCol, m.maxColOnLine(m.outCursorLine))
	// Keep cursor on screen after window scroll (e.g. clamped at buffer edges).
	top, bot := m.outputScrollY, m.outputScrollY+h
	if m.outCursorLine < top {
		m.outCursorLine = top
	}
	if h > 0 && m.outCursorLine >= bot {
		m.outCursorLine = bot - 1
	}
	m.outCursorCol = min(m.outCursorCol, m.maxColOnLine(m.outCursorLine))
	m.scrollOutputToShowCursor()
}

func applyOutputCursor(line string, col int) string {
	rev := lipgloss.NewStyle().Reverse(true)
	sw := ansi.StringWidth(line)
	if sw == 0 {
		return rev.Render(" ")
	}
	if col >= sw {
		col = sw - 1
	}
	before := ansi.Cut(line, 0, col)
	cursorCell := ansi.Cut(line, col, col+1)
	if cursorCell == "" {
		cursorCell = " "
	}
	after := ansi.TruncateLeft(line, col+1, "")
	return before + rev.Render(cursorCell) + after
}

func (m Model) renderOutputView() string {
	h := m.outputViewHeight()
	w := m.width
	if h <= 0 || w <= 0 {
		return ""
	}
	if len(m.outputLines) == 0 {
		return lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).MaxWidth(w).Render("")
	}
	top := max(0, m.outputScrollY)
	end := min(top+h, len(m.outputLines))
	chunk := m.outputLines[top:end]
	out := make([]string, len(chunk))
	showCursor := m.focused && m.mode == ModeOutput
	for i, line := range chunk {
		global := top + i
		styled := line
		if showCursor && global == m.outCursorLine {
			styled = applyOutputCursor(line, m.outCursorCol)
		}
		if m.outputScrollX == 0 && ansi.StringWidth(styled) <= w {
			out[i] = styled
		} else {
			out[i] = ansi.Cut(styled, m.outputScrollX, m.outputScrollX+w)
		}
	}
	contents := lipgloss.NewStyle().
		Width(w).
		Height(h).
		MaxHeight(h).
		MaxWidth(w).
		Render(strings.Join(out, "\n"))
	return contents
}

func (m Model) outputScrollPercent() float64 {
	h := m.outputViewHeight()
	n := len(m.outputLines)
	if h <= 0 || n == 0 {
		return 1.0
	}
	if n <= h {
		return 1.0
	}
	y := float64(m.outputScrollY)
	maxY := float64(n - h)
	v := y / maxY
	return max(0.0, min(1.0, v))
}

func (m *Model) SetFocused(f bool) {
	m.focused = f
	if f && m.mode == ModeInput {
		m.textarea.Focus()
	} else {
		m.textarea.Blur()
	}
}

// IsInputMode returns true when the AI pane is actively receiving text input.
func (m Model) IsInputMode() bool {
	return m.focused && (m.mode == ModeInput || m.showOverlay)
}

func (m *Model) SetConnKey(connKey string) {
	if m.connKey != connKey {
		m.connKey = connKey
		m.refreshViewport()
	}
}

// wrapText wraps plain text (non-code) at the given column width.
func wrapText(s string, width int) string {
	if width <= 8 {
		return s
	}
	var out strings.Builder
	for _, para := range strings.Split(s, "\n") {
		out.WriteString(wrapLine(para, width))
		out.WriteByte('\n')
	}
	result := out.String()
	// trim trailing newline we added
	return strings.TrimRight(result, "\n")
}

func wrapLine(s string, width int) string {
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	var out strings.Builder
	lineLen := 0
	inWord := false
	var wordBuf strings.Builder

	flushWord := func() {
		word := wordBuf.String()
		wordBuf.Reset()
		wl := utf8.RuneCountInString(word)
		if lineLen > 0 {
			if lineLen+1+wl > width {
				out.WriteByte('\n')
				lineLen = 0
			} else {
				out.WriteByte(' ')
				lineLen++
			}
		}
		out.WriteString(word)
		lineLen += wl
	}

	for _, r := range s {
		if r == ' ' || r == '\t' {
			if inWord {
				flushWord()
				inWord = false
			}
		} else {
			wordBuf.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		flushWord()
	}
	return out.String()
}

// renderMessage renders a single chat message with word-wrap and code block highlighting.
// If isActiveSQL is true, the last SQL code block is rendered with an accent border
// (indicating it will be extracted when enter is pressed in output mode).
func (m Model) renderMessage(role, content string, isActiveSQL bool) string {
	w := m.width
	if w <= 4 {
		w = 4
	}

	var prefix string
	var prefixStyle lipgloss.Style
	if role == "user" {
		prefix = "You: "
		prefixStyle = m.theme.Success
	} else {
		prefix = "AI:  "
		prefixStyle = m.theme.Bold
	}

	// Normal code block style (dim)
	normalCodeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(w - 4)

	// Active/highlighted SQL block style (accent)
	activeCodeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.BorderFocused.GetBorderTopForeground()).
		Width(w - 4)

	var sb strings.Builder
	sb.WriteString(prefixStyle.Render(prefix))
	sb.WriteByte('\n')

	remaining := content
	sqlBlockCount := strings.Count(content, "```sql")
	currentBlock := 0

	for {
		startMark := "```sql"
		startIdx := strings.Index(remaining, startMark)
		if startIdx == -1 {
			sb.WriteString(wrapText(strings.TrimSpace(remaining), w))
			break
		}
		before := strings.TrimSpace(remaining[:startIdx])
		if before != "" {
			sb.WriteString(wrapText(before, w))
			sb.WriteByte('\n')
		}
		after := remaining[startIdx+len(startMark):]
		endIdx := strings.Index(after, "```")
		if endIdx == -1 {
			sb.WriteString(wrapText(strings.TrimSpace(after), w))
			break
		}
		currentBlock++
		code := strings.TrimSpace(after[:endIdx])
		sb.WriteByte('\n')
		// Highlight only the last SQL block in the last AI msg
		if isActiveSQL && currentBlock == sqlBlockCount {
			sb.WriteString(activeCodeStyle.Render(code))
		} else {
			sb.WriteString(normalCodeStyle.Render(code))
		}
		sb.WriteByte('\n')
		remaining = after[endIdx+3:]
	}

	return sb.String()
}

func (m *Model) refreshViewport() {
	if m.Store == nil || m.connKey == "" {
		m.outputLines = []string{""}
		m.hasSQL = false
		m.outputScrollY = 0
		m.outputScrollX = 0
		m.outCursorLine = 0
		m.outCursorCol = 0
		return
	}
	chat := m.Store.GetSession(m.connKey)
	var sb strings.Builder
	m.hasSQL = false

	// Find index of last AI message that has a SQL block
	lastSQLIdx := -1
	for i, msg := range chat.Messages {
		if msg.Role == "ai" && strings.Contains(msg.Content, "```sql") {
			lastSQLIdx = i
			m.hasSQL = true
		}
	}

	for i, msg := range chat.Messages {
		isActiveSQL := (i == lastSQLIdx)
		sb.WriteString(m.renderMessage(msg.Role, msg.Content, isActiveSQL))
		sb.WriteString("\n\n")
	}
	content := sb.String()
	m.outputLines = strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	m.outputScrollY = m.maxOutputScrollY()
	m.outCursorLine = max(0, len(m.outputLines)-1)
	m.outCursorCol = m.maxColOnLine(m.outCursorLine)
	m.clampOutCursorToBuffer()
	m.scrollOutputToShowCursor()
}

func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

// rebuildFiltered rebuilds overlayFiltered based on overlayQuery.
func (m *Model) rebuildFiltered() {
	m.overlayFiltered = nil
	for _, it := range m.overlayAllItems {
		if m.overlayQuery == "" || strings.Contains(strings.ToLower(it), strings.ToLower(m.overlayQuery)) {
			m.overlayFiltered = append(m.overlayFiltered, it)
		}
	}
	if m.overlayCursor >= len(m.overlayFiltered) {
		m.overlayCursor = len(m.overlayFiltered) - 1
	}
	if m.overlayCursor < 0 {
		m.overlayCursor = 0
	}
}

func (m Model) Update(msg tea.Msg, schemaTables, schemaCols []string) (Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case AIResponseMsg:
		m.loading = false
		if msg.Err != nil {
			m.Store.AppendAIMessage(m.connKey, "Error: "+msg.Err.Error())
		} else {
			m.Store.AppendAIMessage(m.connKey, msg.Response)
		}
		m.refreshViewport()
		return m, nil

	case tea.MouseMsg:
		if !m.focused || m.mode != ModeOutput || len(m.outputLines) == 0 {
			return m, nil
		}
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		const wheelDelta = 3
		switch msg.Button { //nolint:exhaustive
		case tea.MouseButtonWheelUp:
			if msg.Shift {
				m.moveOutCursorCol(-4)
			} else {
				m.scrollOutputWindow(-wheelDelta)
			}
			return m, nil
		case tea.MouseButtonWheelDown:
			if msg.Shift {
				m.moveOutCursorCol(4)
			} else {
				m.scrollOutputWindow(wheelDelta)
			}
			return m, nil
		case tea.MouseButtonWheelLeft:
			m.moveOutCursorCol(-4)
			return m, nil
		case tea.MouseButtonWheelRight:
			m.moveOutCursorCol(4)
			return m, nil
		}
		return m, nil

	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}

		if m.showOverlay {
			cmd := m.handleOverlayKey(msg)
			return m, cmd
		}

		if m.mode == ModeOutput {
			switch msg.String() {
			case "i":
				m.mode = ModeInput
				m.textarea.Focus()
				return m, nil
			case "enter":
				sql := m.extractLastSQL()
				if sql != "" {
					cmds = append(cmds, func() tea.Msg {
						return ExtractSQLMsg{SQL: sql}
					})
				}
				return m, tea.Batch(cmds...)
			}
			vk := viewport.DefaultKeyMap()
			switch {
			case key.Matches(msg, vk.Down):
				m.moveOutCursorLine(1)
			case key.Matches(msg, vk.Up):
				m.moveOutCursorLine(-1)
			case key.Matches(msg, vk.Left):
				m.moveOutCursorCol(-1)
			case key.Matches(msg, vk.Right):
				m.moveOutCursorCol(1)
			case key.Matches(msg, vk.PageDown):
				m.scrollOutputWindow(m.outputViewHeight())
			case key.Matches(msg, vk.PageUp):
				m.scrollOutputWindow(-m.outputViewHeight())
			case key.Matches(msg, vk.HalfPageDown):
				m.scrollOutputWindow(max(1, m.outputViewHeight()/2))
			case key.Matches(msg, vk.HalfPageUp):
				m.scrollOutputWindow(-max(1, m.outputViewHeight()/2))
			default:
				return m, nil
			}
			return m, nil
		}

		if m.mode == ModeInput {
			switch msg.String() {
			case "esc":
				m.mode = ModeOutput
				m.textarea.Blur()
				return m, nil
			case "enter":
				val := strings.TrimSpace(m.textarea.Value())
				if val != "" && !m.loading {
					m.textarea.Reset()
					m.loading = true
					connKey := m.connKey
					cmds = append(cmds, func() tea.Msg {
						return AISendPromptMsg{ConnKey: connKey, Prompt: val}
					})
				}
				return m, tea.Batch(cmds...)
			case "@":
				m.showOverlay = true
				m.overlayType = '@'
				m.overlayQuery = ""
				m.overlayCursor = 0
				m.overlayAllItems = append([]string{"all"}, schemaTables...)
				m.rebuildFiltered()
				return m, nil
			case "#":
				m.showOverlay = true
				m.overlayType = '#'
				m.overlayQuery = ""
				m.overlayCursor = 0
				m.overlayAllItems = schemaCols
				m.rebuildFiltered()
				return m, nil
			}

			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}
	}

	return m, nil
}

func (m *Model) handleOverlayKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.showOverlay = false
	case "down", "j", "ctrl+n":
		if m.overlayCursor < len(m.overlayFiltered)-1 {
			m.overlayCursor++
		}
	case "up", "k", "ctrl+p":
		if m.overlayCursor > 0 {
			m.overlayCursor--
		}
	case "enter":
		if m.overlayCursor >= 0 && m.overlayCursor < len(m.overlayFiltered) {
			item := m.overlayFiltered[m.overlayCursor]
			val := m.textarea.Value()
			m.textarea.SetValue(val + string(m.overlayType) + item + " ")
			m.textarea.SetCursor(len(m.textarea.Value()))
		}
		m.showOverlay = false
	case "backspace":
		if len(m.overlayQuery) > 0 {
			m.overlayQuery = m.overlayQuery[:len(m.overlayQuery)-1]
			m.rebuildFiltered()
		} else {
			m.showOverlay = false
		}
	default:
		if len(msg.Runes) > 0 {
			m.overlayQuery += string(msg.Runes[0])
			m.overlayCursor = 0
			m.rebuildFiltered()
		}
	}
	return nil
}

// AppendUserMessage adds a user message to the store and refreshes the viewport.
func (m *Model) AppendUserMessage(text string) {
	if m.Store != nil && m.connKey != "" {
		m.Store.AppendUserMessage(m.connKey, text)
		m.refreshViewport()
	}
}

func (m Model) extractLastSQL() string {
	if m.Store == nil {
		return ""
	}
	chat := m.Store.GetSession(m.connKey)
	for i := len(chat.Messages) - 1; i >= 0; i-- {
		msg := chat.Messages[i]
		if msg.Role == "ai" {
			start := strings.Index(msg.Content, "```sql")
			if start != -1 {
				rem := msg.Content[start+6:]
				end := strings.Index(rem, "```")
				if end != -1 {
					return strings.TrimSpace(rem[:end])
				}
			}
		}
	}
	return ""
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	// Status line
	var statusParts []string
	if m.Store != nil && m.Store.Cfg != nil && m.Store.Cfg.AI != nil {
		sz := m.Store.GetConversationSize(m.connKey)
		szKB := sz / 1024
		sizeStr := fmt.Sprintf("%dKB", szKB)
		if m.Store.Cfg.AI.MaxHistorySizeKB > 0 && szKB >= m.Store.Cfg.AI.MaxHistorySizeKB {
			sizeStr += " ⚠"
		}
		statusParts = append(statusParts, sizeStr)
	}
	// Scroll position
	scrollPct := int(m.outputScrollPercent() * 100)
	statusParts = append(statusParts, fmt.Sprintf("%d%%", scrollPct))

	if m.mode == ModeInput {
		statusParts = append(statusParts, "INSERT — esc:scroll  enter:send  @:tables  #:cols")
	} else {
		if m.hasSQL {
			statusParts = append(statusParts, "NORMAL — i:type  ↑↓/hjkl:cursor  [enter]:copy SQL→editor ◀")
		} else {
			statusParts = append(statusParts, "NORMAL — i:type  ↑↓/hjkl:cursor")
		}
	}
	statusLine := m.theme.StatusBar.Width(m.width).Render(strings.Join(statusParts, " | "))

	vpView := m.renderOutputView()

	var inputView string
	if m.loading {
		inputView = m.theme.Dimmed.Width(m.width).Height(inputH).Render("⏳ Waiting for AI response...")
	} else {
		inputView = m.textarea.View()
	}

	if m.showOverlay && !m.loading {
		overlayBox := m.renderOverlay()
		inputView = overlayBox + "\n" + inputView
	}

	inner := lipgloss.JoinVertical(lipgloss.Left, vpView, statusLine, inputView)
	return lipgloss.NewStyle().Width(m.width).Height(m.height).MaxHeight(m.height).Render(inner)
}

func (m Model) renderOverlay() string {
	if len(m.overlayFiltered) == 0 {
		return m.theme.PaletteBox.Render("No match")
	}
	maxItems := 6
	var sb strings.Builder
	for i, it := range m.overlayFiltered {
		if i >= maxItems {
			break
		}
		if i == m.overlayCursor {
			sb.WriteString(lipgloss.NewStyle().Reverse(true).Render(it) + "\n")
		} else {
			sb.WriteString(it + "\n")
		}
	}
	return m.theme.PaletteBox.Render(sb.String())
}
