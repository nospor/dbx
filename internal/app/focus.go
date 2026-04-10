package app

// Panel identifies which panel currently has keyboard focus.
type Panel int

const (
	PanelExplorer Panel = iota
	PanelEditor
	PanelResults
	PanelAI
)

func (p Panel) String() string {
	switch p {
	case PanelExplorer:
		return "explorer"
	case PanelEditor:
		return "editor"
	case PanelResults:
		return "results"
	case PanelAI:
		return "ai"
	}
	return "unknown"
}
