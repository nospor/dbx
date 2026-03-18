package app

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines global keybindings that work regardless of focused panel.
type KeyMap struct {
	FocusExplorer key.Binding
	FocusEditor   key.Binding
	FocusResults  key.Binding
	MasterKey     key.Binding
	Quit          key.Binding
	Help          key.Binding
}

// DefaultKeyMap returns the default global keybindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		FocusExplorer: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "focus explorer"),
		),
		FocusEditor: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "focus editor"),
		),
		FocusResults: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "focus results"),
		),
		MasterKey: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "command palette"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
	}
}
