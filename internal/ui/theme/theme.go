package theme

import "github.com/charmbracelet/lipgloss"

// Theme holds all color and style definitions used across the UI.
type Theme struct {
	Name string

	// Borders & chrome
	BorderFocused   lipgloss.Style
	BorderUnfocused lipgloss.Style

	// Status bar
	StatusBar     lipgloss.Style
	StatusBarMode lipgloss.Style

	// Text
	Normal  lipgloss.Style
	Dimmed  lipgloss.Style
	Bold    lipgloss.Style
	Error   lipgloss.Style
	Success lipgloss.Style

	// Explorer tree
	TreeSelected   lipgloss.Style
	TreeConnection lipgloss.Style
	TreeDatabase   lipgloss.Style
	TreeTable      lipgloss.Style
	TreeColumn     lipgloss.Style

	// Command palette
	PaletteBox     lipgloss.Style
	PaletteTitle   lipgloss.Style
	PaletteItem    lipgloss.Style
	PaletteKey     lipgloss.Style

	// Results table
	TableHeader lipgloss.Style
	TableRow    lipgloss.Style
	TableRowAlt lipgloss.Style
	TableCursor lipgloss.Style
}

// Terminal theme — uses terminal default colors so it works with any terminal palette.
func Terminal() Theme {
	focused := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12"))

	unfocused := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8"))

	return Theme{
		Name:            "terminal",
		BorderFocused:   focused,
		BorderUnfocused: unfocused,

		StatusBar:     lipgloss.NewStyle().Background(lipgloss.Color("8")).Foreground(lipgloss.Color("15")).Padding(0, 1),
		StatusBarMode: lipgloss.NewStyle().Background(lipgloss.Color("4")).Foreground(lipgloss.Color("15")).Bold(true).Padding(0, 1),

		Normal:  lipgloss.NewStyle(),
		Dimmed:  lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		Bold:    lipgloss.NewStyle().Bold(true),
		Error:   lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
		Success: lipgloss.NewStyle().Foreground(lipgloss.Color("2")),

		TreeSelected:   lipgloss.NewStyle().Background(lipgloss.Color("4")).Foreground(lipgloss.Color("15")),
		TreeConnection: lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true),
		TreeDatabase:   lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		TreeTable:      lipgloss.NewStyle().Foreground(lipgloss.Color("15")),
		TreeColumn:     lipgloss.NewStyle().Foreground(lipgloss.Color("8")),

		PaletteBox:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12")).Padding(0, 1),
		PaletteTitle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")),
		PaletteItem:  lipgloss.NewStyle().Foreground(lipgloss.Color("15")),
		PaletteKey:   lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true),

		TableHeader: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).Underline(true),
		TableRow:    lipgloss.NewStyle(),
		TableRowAlt: lipgloss.NewStyle().Foreground(lipgloss.Color("7")),
		TableCursor: lipgloss.NewStyle().Background(lipgloss.Color("4")).Foreground(lipgloss.Color("15")),
	}
}

// Dark theme — explicit dark background colors.
func Dark() Theme {
	t := Terminal()
	t.Name = "dark"

	t.BorderFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#7aa2f7"))
	t.BorderUnfocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#3b4261"))

	t.StatusBar = lipgloss.NewStyle().Background(lipgloss.Color("#1a1b26")).Foreground(lipgloss.Color("#a9b1d6")).Padding(0, 1)
	t.StatusBarMode = lipgloss.NewStyle().Background(lipgloss.Color("#7aa2f7")).Foreground(lipgloss.Color("#1a1b26")).Bold(true).Padding(0, 1)

	t.Normal = lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6"))
	t.Dimmed = lipgloss.NewStyle().Foreground(lipgloss.Color("#3b4261"))
	t.Error = lipgloss.NewStyle().Foreground(lipgloss.Color("#f7768e")).Bold(true)
	t.Success = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ece6a"))

	t.TreeSelected = lipgloss.NewStyle().Background(lipgloss.Color("#283457")).Foreground(lipgloss.Color("#7aa2f7"))
	t.TreeConnection = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68")).Bold(true)
	t.TreeDatabase = lipgloss.NewStyle().Foreground(lipgloss.Color("#7dcfff"))
	t.TreeTable = lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6"))
	t.TreeColumn = lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))

	t.PaletteBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#7aa2f7")).Background(lipgloss.Color("#1a1b26")).Padding(0, 1)
	t.PaletteTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7aa2f7"))
	t.PaletteItem = lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6"))
	t.PaletteKey = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68")).Bold(true)

	t.TableHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7aa2f7")).Underline(true)
	t.TableCursor = lipgloss.NewStyle().Background(lipgloss.Color("#283457")).Foreground(lipgloss.Color("#7aa2f7"))

	return t
}

// Light theme — explicit light background colors.
func Light() Theme {
	t := Terminal()
	t.Name = "light"

	t.BorderFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#2e7de9"))
	t.BorderUnfocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#9ca3af"))

	t.StatusBar = lipgloss.NewStyle().Background(lipgloss.Color("#e9e9ed")).Foreground(lipgloss.Color("#3760bf")).Padding(0, 1)
	t.StatusBarMode = lipgloss.NewStyle().Background(lipgloss.Color("#2e7de9")).Foreground(lipgloss.Color("#ffffff")).Bold(true).Padding(0, 1)

	t.Normal = lipgloss.NewStyle().Foreground(lipgloss.Color("#3760bf"))
	t.Dimmed = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af"))
	t.Error = lipgloss.NewStyle().Foreground(lipgloss.Color("#f52a65")).Bold(true)
	t.Success = lipgloss.NewStyle().Foreground(lipgloss.Color("#587539"))

	t.TreeSelected = lipgloss.NewStyle().Background(lipgloss.Color("#b6bfe2")).Foreground(lipgloss.Color("#2e7de9"))
	t.TreeConnection = lipgloss.NewStyle().Foreground(lipgloss.Color("#8c6c3e")).Bold(true)
	t.TreeDatabase = lipgloss.NewStyle().Foreground(lipgloss.Color("#007197"))
	t.TreeTable = lipgloss.NewStyle().Foreground(lipgloss.Color("#3760bf"))
	t.TreeColumn = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca3af"))

	t.PaletteBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#2e7de9")).Background(lipgloss.Color("#e9e9ed")).Padding(0, 1)
	t.PaletteTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#2e7de9"))
	t.PaletteItem = lipgloss.NewStyle().Foreground(lipgloss.Color("#3760bf"))
	t.PaletteKey = lipgloss.NewStyle().Foreground(lipgloss.Color("#8c6c3e")).Bold(true)

	t.TableHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#2e7de9")).Underline(true)
	t.TableCursor = lipgloss.NewStyle().Background(lipgloss.Color("#b6bfe2")).Foreground(lipgloss.Color("#2e7de9"))

	return t
}

// Get returns the theme matching the given name (falls back to Terminal).
func Get(name string) Theme {
	switch name {
	case "dark":
		return Dark()
	case "light":
		return Light()
	default:
		return Terminal()
	}
}
