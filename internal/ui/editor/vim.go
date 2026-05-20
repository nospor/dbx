package editor

// Mode represents the vim editing mode.
type Mode int

const (
	ModeNormal Mode = iota
	ModeInsert
)

func (m Mode) String() string {
	switch m {
	case ModeInsert:
		return "INSERT"
	default:
		return "NORMAL"
	}
}

// VimState holds the current vim mode and cursor position.
type VimState struct {
	mode     Mode
	row      int
	col      int
	pendingG bool
	pendingD bool
	pendingY bool
	pendingC bool
}

func newVimState() *VimState {
	return &VimState{mode: ModeNormal}
}
