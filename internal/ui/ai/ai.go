package ai

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

	viewport viewport.Model
	textarea textarea.Model

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

	vp := viewport.New(0, 0)
	vp.YPosition = 0

	return Model{
		theme:    t,
		mode:     ModeOutput,
		Store:    store,
		textarea: ta,
		viewport: vp,
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
	vpH := m.height - statusH - inputH - 1
	if vpH < 0 {
		vpH = 0
	}
	m.viewport.Width = m.width
	m.viewport.Height = vpH
	m.textarea.SetWidth(m.width)
	m.textarea.SetHeight(inputH)
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
		m.viewport.SetContent("")
		m.hasSQL = false
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
	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
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
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
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
	scrollPct := int(m.viewport.ScrollPercent() * 100)
	statusParts = append(statusParts, fmt.Sprintf("%d%%", scrollPct))

	if m.mode == ModeInput {
		statusParts = append(statusParts, "INSERT — esc:scroll  enter:send  @:tables  #:cols")
	} else {
		if m.hasSQL {
			statusParts = append(statusParts, "NORMAL — i:type  ↑↓/hjkl:scroll  [enter]:copy SQL→editor ◀")
		} else {
			statusParts = append(statusParts, "NORMAL — i:type  ↑↓/hjkl:scroll")
		}
	}
	statusLine := m.theme.StatusBar.Width(m.width).Render(strings.Join(statusParts, " | "))

	vpView := m.viewport.View()

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
