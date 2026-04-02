package cmdpalette

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/robertn/dbx/internal/ui/theme"
)

// Command represents a single command shown in the palette.
type Command struct {
	Key         string
	Description string
	Action      func() tea.Msg
}

// ExecuteCommandMsg is sent when a command is selected.
type ExecuteCommandMsg struct {
	Action func() tea.Msg
}

// Model is the bubbletea model for the command palette overlay.
type Model struct {
	theme    theme.Theme
	visible  bool
	commands []Command
	cursor   int
	title    string
}

// New creates a new palette model.
func New(t theme.Theme) Model {
	return Model{theme: t}
}

func (m *Model) SetTheme(t theme.Theme) {
	m.theme = t
}

// Show opens the palette with the given title and commands.
func (m *Model) Show(title string, commands []Command) {
	m.title = title
	m.commands = commands
	m.cursor = 0
	m.visible = true
}

// Hide closes the palette.
func (m *Model) Hide() {
	m.visible = false
}

func (m Model) IsVisible() bool {
	return m.visible
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", " ":
			m.visible = false
		case "j", "down":
			if m.cursor < len(m.commands)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter":
			if m.cursor < len(m.commands) {
				cmd := m.commands[m.cursor]
				m.visible = false
				if cmd.Action != nil {
					return m, func() tea.Msg { return ExecuteCommandMsg{Action: cmd.Action} }
				}
			}
		default:
			// Allow single-key shortcuts
			for _, cmd := range m.commands {
				if msg.String() == cmd.Key {
					m.visible = false
					if cmd.Action != nil {
						return m, func() tea.Msg { return ExecuteCommandMsg{Action: cmd.Action} }
					}
				}
			}
		}
	}
	return m, nil
}

// View renders the palette as a string to be overlaid in the bottom-right corner.
func (m Model) View() string {
	if !m.visible || len(m.commands) == 0 {
		return ""
	}

	// Fixed inner width so the box does not jump when j/k changes selection.
	gap1 := m.theme.PaletteFill.Render(" ")
	gap2 := m.theme.PaletteFill.Render("  ")
	innerW := lipgloss.Width(m.theme.PaletteTitle.Render(m.title))
	for _, cmd := range m.commands {
		key := m.theme.PaletteKey.Render(cmd.Key)
		desc := m.theme.PaletteItem.Render(cmd.Description)
		if w := lipgloss.Width(gap1 + key + gap2 + desc); w > innerW {
			innerW = w
		}
		plain := " " + cmd.Key + "  " + cmd.Description
		if w := lipgloss.Width(plain); w > innerW {
			innerW = w
		}
	}
	if innerW < 12 {
		innerW = 12
	}

	row := m.theme.PaletteFill.Copy().Width(innerW).Align(lipgloss.Left)

	var sb strings.Builder
	sb.WriteString(row.Render(m.theme.PaletteTitle.Render(m.title)) + "\n")

	for i, cmd := range m.commands {
		key := m.theme.PaletteKey.Render(cmd.Key)
		desc := m.theme.PaletteItem.Render(cmd.Description)
		var line string
		if i == m.cursor {
			line = lipgloss.NewStyle().Reverse(true).Width(innerW).Align(lipgloss.Left).Render(
				" " + cmd.Key + "  " + cmd.Description)
		} else {
			line = row.Render(gap1 + key + gap2 + desc)
		}
		sb.WriteString(line + "\n")
	}

	return m.theme.PaletteBox.Render(sb.String())
}
