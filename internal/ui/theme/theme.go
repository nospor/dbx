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
	TableHeader       lipgloss.Style
	TableHeaderActive lipgloss.Style // header cell for the cursor column
	TableRow          lipgloss.Style
	TableRowAlt       lipgloss.Style
	TableCursorRow    lipgloss.Style // non-active cells on the cursor row
	TableCursorCell   lipgloss.Style // the focused cell (stronger than row)
	TableRowSelected  lipgloss.Style // marked rows in results (s / S)
}

// Terminal theme — uses terminal default colors so it works with any terminal palette.
func Terminal() Theme {
	accentBg := lipgloss.Color("6")       // calmer cyan instead of vivid blue
	accentFg := lipgloss.Color("0")       // dark text on accent backgrounds
	accentSoftBg := lipgloss.Color("8")   // muted dark-gray selection background
	accentSoftFg := lipgloss.Color("15")  // light text for contrast

	focused := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentBg)

	unfocused := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8"))

	return Theme{
		Name:            "terminal",
		BorderFocused:   focused,
		BorderUnfocused: unfocused,

		StatusBar:     lipgloss.NewStyle().Background(lipgloss.Color("8")).Foreground(lipgloss.Color("15")).Padding(0, 1),
		StatusBarMode: lipgloss.NewStyle().Background(accentBg).Foreground(accentFg).Bold(true).Padding(0, 1),

		Normal:  lipgloss.NewStyle(),
		Dimmed:  lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		Bold:    lipgloss.NewStyle().Bold(true),
		Error:   lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
		Success: lipgloss.NewStyle().Foreground(lipgloss.Color("2")),

		TreeSelected:   lipgloss.NewStyle().Background(accentSoftBg).Foreground(accentSoftFg),
		TreeConnection: lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true),
		TreeDatabase:   lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		TreeTable:      lipgloss.NewStyle().Foreground(lipgloss.Color("15")),
		TreeColumn:     lipgloss.NewStyle().Foreground(lipgloss.Color("8")),

		PaletteBox:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accentBg).Padding(0, 1),
		PaletteTitle: lipgloss.NewStyle().Bold(true).Foreground(accentBg),
		PaletteItem:  lipgloss.NewStyle().Foreground(lipgloss.Color("15")),
		PaletteKey:   lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true),

		TableHeader:       lipgloss.NewStyle().Bold(true).Foreground(accentBg).Underline(true),
		TableHeaderActive: lipgloss.NewStyle().Bold(true).Foreground(accentFg).Background(accentBg).Underline(true),
		TableRow:          lipgloss.NewStyle(),
		TableRowAlt:       lipgloss.NewStyle().Foreground(lipgloss.Color("7")),
		TableCursorRow:    lipgloss.NewStyle().Background(lipgloss.Color("0")).Foreground(lipgloss.Color("7")),
		TableCursorCell:   lipgloss.NewStyle().Background(accentSoftBg).Foreground(accentSoftFg).Bold(true),
		TableRowSelected:  lipgloss.NewStyle().Background(lipgloss.Color("22")).Foreground(lipgloss.Color("7")),
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
	t.TableHeaderActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7aa2f7")).Background(lipgloss.Color("#283457")).Underline(true)
	t.TableCursorRow = lipgloss.NewStyle().Background(lipgloss.Color("#1f2335")).Foreground(lipgloss.Color("#a9b1d6"))
	t.TableCursorCell = lipgloss.NewStyle().Background(lipgloss.Color("#283457")).Foreground(lipgloss.Color("#7aa2f7")).Bold(true)
	t.TableRowSelected = lipgloss.NewStyle().Background(lipgloss.Color("#283d22")).Foreground(lipgloss.Color("#a9b1d6"))

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
	t.TableHeaderActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#2e7de9")).Background(lipgloss.Color("#b6bfe2")).Underline(true)
	t.TableCursorRow = lipgloss.NewStyle().Background(lipgloss.Color("#d8dce8")).Foreground(lipgloss.Color("#3760bf"))
	t.TableCursorCell = lipgloss.NewStyle().Background(lipgloss.Color("#b6bfe2")).Foreground(lipgloss.Color("#2e7de9")).Bold(true)
	t.TableRowSelected = lipgloss.NewStyle().Background(lipgloss.Color("#c2ddc2")).Foreground(lipgloss.Color("#3760bf"))

	return t
}

// CatppuccinMocha theme — popular Catppuccin dark variant.
func CatppuccinMocha() Theme {
	t := Terminal()
	t.Name = "catppuccin-mocha"

	t.BorderFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#89b4fa"))
	t.BorderUnfocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#45475a"))

	t.StatusBar = lipgloss.NewStyle().Background(lipgloss.Color("#1e1e2e")).Foreground(lipgloss.Color("#cdd6f4")).Padding(0, 1)
	t.StatusBarMode = lipgloss.NewStyle().Background(lipgloss.Color("#89b4fa")).Foreground(lipgloss.Color("#1e1e2e")).Bold(true).Padding(0, 1)

	t.Normal = lipgloss.NewStyle().Foreground(lipgloss.Color("#cdd6f4"))
	t.Dimmed = lipgloss.NewStyle().Foreground(lipgloss.Color("#6c7086"))
	t.Error = lipgloss.NewStyle().Foreground(lipgloss.Color("#f38ba8")).Bold(true)
	t.Success = lipgloss.NewStyle().Foreground(lipgloss.Color("#a6e3a1"))

	t.TreeSelected = lipgloss.NewStyle().Background(lipgloss.Color("#313244")).Foreground(lipgloss.Color("#89b4fa"))
	t.TreeConnection = lipgloss.NewStyle().Foreground(lipgloss.Color("#f9e2af")).Bold(true)
	t.TreeDatabase = lipgloss.NewStyle().Foreground(lipgloss.Color("#94e2d5"))
	t.TreeTable = lipgloss.NewStyle().Foreground(lipgloss.Color("#cdd6f4"))
	t.TreeColumn = lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))

	t.PaletteBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#89b4fa")).Background(lipgloss.Color("#1e1e2e")).Padding(0, 1)
	t.PaletteTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#89b4fa"))
	t.PaletteItem = lipgloss.NewStyle().Foreground(lipgloss.Color("#cdd6f4"))
	t.PaletteKey = lipgloss.NewStyle().Foreground(lipgloss.Color("#f9e2af")).Bold(true)

	t.TableHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#89b4fa")).Underline(true)
	t.TableHeaderActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#89b4fa")).Background(lipgloss.Color("#313244")).Underline(true)
	t.TableCursorRow = lipgloss.NewStyle().Background(lipgloss.Color("#181825")).Foreground(lipgloss.Color("#cdd6f4"))
	t.TableCursorCell = lipgloss.NewStyle().Background(lipgloss.Color("#313244")).Foreground(lipgloss.Color("#89b4fa")).Bold(true)
	t.TableRowSelected = lipgloss.NewStyle().Background(lipgloss.Color("#304428")).Foreground(lipgloss.Color("#a6e3a1"))

	return t
}

// CatppuccinLatte theme — popular Catppuccin light variant.
func CatppuccinLatte() Theme {
	t := Terminal()
	t.Name = "catppuccin-latte"

	t.BorderFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#1e66f5"))
	t.BorderUnfocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#9ca0b0"))

	t.StatusBar = lipgloss.NewStyle().Background(lipgloss.Color("#eff1f5")).Foreground(lipgloss.Color("#4c4f69")).Padding(0, 1)
	t.StatusBarMode = lipgloss.NewStyle().Background(lipgloss.Color("#1e66f5")).Foreground(lipgloss.Color("#eff1f5")).Bold(true).Padding(0, 1)

	t.Normal = lipgloss.NewStyle().Foreground(lipgloss.Color("#4c4f69"))
	t.Dimmed = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca0b0"))
	t.Error = lipgloss.NewStyle().Foreground(lipgloss.Color("#d20f39")).Bold(true)
	t.Success = lipgloss.NewStyle().Foreground(lipgloss.Color("#40a02b"))

	t.TreeSelected = lipgloss.NewStyle().Background(lipgloss.Color("#ccd0da")).Foreground(lipgloss.Color("#1e66f5"))
	t.TreeConnection = lipgloss.NewStyle().Foreground(lipgloss.Color("#df8e1d")).Bold(true)
	t.TreeDatabase = lipgloss.NewStyle().Foreground(lipgloss.Color("#179299"))
	t.TreeTable = lipgloss.NewStyle().Foreground(lipgloss.Color("#4c4f69"))
	t.TreeColumn = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ca0b0"))

	t.PaletteBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#1e66f5")).Background(lipgloss.Color("#eff1f5")).Padding(0, 1)
	t.PaletteTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1e66f5"))
	t.PaletteItem = lipgloss.NewStyle().Foreground(lipgloss.Color("#4c4f69"))
	t.PaletteKey = lipgloss.NewStyle().Foreground(lipgloss.Color("#df8e1d")).Bold(true)

	t.TableHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1e66f5")).Underline(true)
	t.TableHeaderActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1e66f5")).Background(lipgloss.Color("#ccd0da")).Underline(true)
	t.TableCursorRow = lipgloss.NewStyle().Background(lipgloss.Color("#e6e9ef")).Foreground(lipgloss.Color("#4c4f69"))
	t.TableCursorCell = lipgloss.NewStyle().Background(lipgloss.Color("#ccd0da")).Foreground(lipgloss.Color("#1e66f5")).Bold(true)
	t.TableRowSelected = lipgloss.NewStyle().Background(lipgloss.Color("#b8e8c5")).Foreground(lipgloss.Color("#4c4f69"))

	return t
}

// Nord theme — inspired by the Nord color palette.
func Nord() Theme {
	t := Terminal()
	t.Name = "nord"

	t.BorderFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#81a1c1"))
	t.BorderUnfocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#4c566a"))

	t.StatusBar = lipgloss.NewStyle().Background(lipgloss.Color("#2e3440")).Foreground(lipgloss.Color("#d8dee9")).Padding(0, 1)
	t.StatusBarMode = lipgloss.NewStyle().Background(lipgloss.Color("#88c0d0")).Foreground(lipgloss.Color("#2e3440")).Bold(true).Padding(0, 1)

	t.Normal = lipgloss.NewStyle().Foreground(lipgloss.Color("#d8dee9"))
	t.Dimmed = lipgloss.NewStyle().Foreground(lipgloss.Color("#4c566a"))
	t.Error = lipgloss.NewStyle().Foreground(lipgloss.Color("#bf616a")).Bold(true)
	t.Success = lipgloss.NewStyle().Foreground(lipgloss.Color("#a3be8c"))

	t.TreeSelected = lipgloss.NewStyle().Background(lipgloss.Color("#3b4252")).Foreground(lipgloss.Color("#81a1c1"))
	t.TreeConnection = lipgloss.NewStyle().Foreground(lipgloss.Color("#ebcb8b")).Bold(true)
	t.TreeDatabase = lipgloss.NewStyle().Foreground(lipgloss.Color("#8fbcbb"))
	t.TreeTable = lipgloss.NewStyle().Foreground(lipgloss.Color("#d8dee9"))
	t.TreeColumn = lipgloss.NewStyle().Foreground(lipgloss.Color("#616e88"))

	t.PaletteBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#81a1c1")).Background(lipgloss.Color("#2e3440")).Padding(0, 1)
	t.PaletteTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#81a1c1"))
	t.PaletteItem = lipgloss.NewStyle().Foreground(lipgloss.Color("#d8dee9"))
	t.PaletteKey = lipgloss.NewStyle().Foreground(lipgloss.Color("#ebcb8b")).Bold(true)

	t.TableHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#81a1c1")).Underline(true)
	t.TableHeaderActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#81a1c1")).Background(lipgloss.Color("#3b4252")).Underline(true)
	t.TableCursorRow = lipgloss.NewStyle().Background(lipgloss.Color("#2a303b")).Foreground(lipgloss.Color("#d8dee9"))
	t.TableCursorCell = lipgloss.NewStyle().Background(lipgloss.Color("#3b4252")).Foreground(lipgloss.Color("#88c0d0")).Bold(true)
	t.TableRowSelected = lipgloss.NewStyle().Background(lipgloss.Color("#3b5323")).Foreground(lipgloss.Color("#d8dee9"))

	return t
}

// GruvboxDark theme — inspired by the Gruvbox dark palette.
func GruvboxDark() Theme {
	t := Terminal()
	t.Name = "gruvbox-dark"

	t.BorderFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#83a598"))
	t.BorderUnfocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#665c54"))

	t.StatusBar = lipgloss.NewStyle().Background(lipgloss.Color("#282828")).Foreground(lipgloss.Color("#ebdbb2")).Padding(0, 1)
	t.StatusBarMode = lipgloss.NewStyle().Background(lipgloss.Color("#458588")).Foreground(lipgloss.Color("#fbf1c7")).Bold(true).Padding(0, 1)

	t.Normal = lipgloss.NewStyle().Foreground(lipgloss.Color("#ebdbb2"))
	t.Dimmed = lipgloss.NewStyle().Foreground(lipgloss.Color("#7c6f64"))
	t.Error = lipgloss.NewStyle().Foreground(lipgloss.Color("#fb4934")).Bold(true)
	t.Success = lipgloss.NewStyle().Foreground(lipgloss.Color("#b8bb26"))

	t.TreeSelected = lipgloss.NewStyle().Background(lipgloss.Color("#3c3836")).Foreground(lipgloss.Color("#83a598"))
	t.TreeConnection = lipgloss.NewStyle().Foreground(lipgloss.Color("#fabd2f")).Bold(true)
	t.TreeDatabase = lipgloss.NewStyle().Foreground(lipgloss.Color("#8ec07c"))
	t.TreeTable = lipgloss.NewStyle().Foreground(lipgloss.Color("#ebdbb2"))
	t.TreeColumn = lipgloss.NewStyle().Foreground(lipgloss.Color("#928374"))

	t.PaletteBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#83a598")).Background(lipgloss.Color("#282828")).Padding(0, 1)
	t.PaletteTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#83a598"))
	t.PaletteItem = lipgloss.NewStyle().Foreground(lipgloss.Color("#ebdbb2"))
	t.PaletteKey = lipgloss.NewStyle().Foreground(lipgloss.Color("#fabd2f")).Bold(true)

	t.TableHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#83a598")).Underline(true)
	t.TableHeaderActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#83a598")).Background(lipgloss.Color("#3c3836")).Underline(true)
	t.TableCursorRow = lipgloss.NewStyle().Background(lipgloss.Color("#32302f")).Foreground(lipgloss.Color("#ebdbb2"))
	t.TableCursorCell = lipgloss.NewStyle().Background(lipgloss.Color("#3c3836")).Foreground(lipgloss.Color("#fabd2f")).Bold(true)
	t.TableRowSelected = lipgloss.NewStyle().Background(lipgloss.Color("#3d4220")).Foreground(lipgloss.Color("#b8bb26"))

	return t
}

// Get returns the theme matching the given name (falls back to Terminal).
func Get(name string) Theme {
	switch name {
	case "dark":
		return Dark()
	case "light":
		return Light()
	case "catppuccin-mocha":
		return CatppuccinMocha()
	case "catppuccin-latte":
		return CatppuccinLatte()
	case "nord":
		return Nord()
	case "gruvbox-dark":
		return GruvboxDark()
	default:
		return Terminal()
	}
}
