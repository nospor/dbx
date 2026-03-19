package explorer

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/robertn/dbx/internal/config"
	"github.com/robertn/dbx/internal/ui/theme"
)

// NodeKind classifies a tree node.
type NodeKind int

const (
	NodeConnection NodeKind = iota
	NodeDatabase
	NodeTable
	NodeView
	NodeColumn
)

// Node is a single item in the explorer tree.
type Node struct {
	Kind     NodeKind
	Label    string
	Detail   string // e.g. column type
	ConnID   string
	DBName   string
	Children []*Node
	Expanded bool
	parent   *Node
}

// Model is the bubbletea model for the explorer panel.
type Model struct {
	cfg     *config.Config
	theme   theme.Theme
	width   int
	height  int
	focused bool

	nodes      []*Node // top-level connection nodes
	flat       []*Node // flattened visible nodes for rendering
	cursor     int
	pendingSel *Node   // set when user presses Enter/s on a node
}

// New creates a new explorer model.
func New(cfg *config.Config, t theme.Theme) Model {
	m := Model{
		cfg:   cfg,
		theme: t,
	}
	m.buildTree()
	return m
}

func (m *Model) buildTree() {
	m.nodes = make([]*Node, 0, len(m.cfg.Connections))
	for _, conn := range m.cfg.Connections {
		n := &Node{
			Kind:   NodeConnection,
			Label:  conn.Name,
			ConnID: conn.ID,
		}
		m.nodes = append(m.nodes, n)
	}
	m.flatten()
}

func (m *Model) flatten() {
	m.flat = nil
	for _, n := range m.nodes {
		m.flattenNode(n)
	}
}

func (m *Model) flattenNode(n *Node) {
	m.flat = append(m.flat, n)
	if n.Expanded {
		for _, child := range n.Children {
			m.flattenNode(child)
		}
	}
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *Model) SetFocused(f bool) {
	m.focused = f
}

func (m *Model) SetConfig(cfg *config.Config) {
	m.cfg = cfg
	m.buildTree()
}

func (m *Model) SetTheme(t theme.Theme) {
	m.theme = t
}

// AddChildrenToNode populates a node's children (called after async schema fetch).
func (m *Model) AddChildrenToNode(connID string, dbName string, children []*Node) {
	for _, n := range m.flat {
		if n.ConnID == connID && n.DBName == dbName {
			n.Children = children
			for _, c := range children {
				c.parent = n
			}
			m.flatten()
			return
		}
	}
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
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(m.flat)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter", "l":
			if len(m.flat) > 0 {
				n := m.flat[m.cursor]
				if len(n.Children) > 0 || n.Kind == NodeConnection || n.Kind == NodeDatabase {
					n.Expanded = !n.Expanded
					m.flatten()
				}
				m.pendingSel = n
			}
		case "s":
			// Quick select: generate SELECT query for table/view
			if len(m.flat) > 0 {
				n := m.flat[m.cursor]
				if n.Kind == NodeTable || n.Kind == NodeView {
					m.pendingSel = &Node{
						Kind:   NodeTable,
						Label:  n.Label,
						ConnID: n.ConnID,
						DBName: n.DBName,
						Detail: "select", // signal to generate SELECT query
					}
				}
			}
		case "g":
			m.cursor = 0
		case "G":
			if len(m.flat) > 0 {
				m.cursor = len(m.flat) - 1
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	var sb strings.Builder

	if len(m.flat) == 0 {
		hint := m.theme.Dimmed.Render("No connections.\nPress space → add connection")
		return lipgloss.NewStyle().Width(m.width).Height(m.height).Render(hint)
	}

	// Match previous inner line budget: was 1 title + (height-2) tree rows (= height-1 rows).
	// Border title moved outside; keep total inner lines at height-1 so the panel doesn't overflow.
	visibleLines := m.height - 1
	if visibleLines < 0 {
		visibleLines = 0
	}
	start := 0
	if m.cursor >= visibleLines {
		start = m.cursor - visibleLines + 1
	}

	for i := start; i < len(m.flat) && i < start+visibleLines; i++ {
		n := m.flat[i]
		line := m.renderNode(n, i == m.cursor)
		sb.WriteString(line + "\n")
	}

	return lipgloss.NewStyle().Width(m.width).Height(m.height).Render(sb.String())
}

func (m Model) renderNode(n *Node, selected bool) string {
	depth := m.nodeDepth(n)
	indent := strings.Repeat("  ", depth)

	var icon string
	var labelStyle lipgloss.Style

	switch n.Kind {
	case NodeConnection:
		if n.Expanded {
			icon = "▼ "
		} else {
			icon = "▶ "
		}
		labelStyle = m.theme.TreeConnection
	case NodeDatabase:
		if n.Expanded {
			icon = "▼ "
		} else {
			icon = "▶ "
		}
		labelStyle = m.theme.TreeDatabase
	case NodeTable:
		if n.Expanded {
			icon = "▼ "
		} else {
			icon = "  "
		}
		labelStyle = m.theme.TreeTable
	case NodeView:
		icon = "  "
		labelStyle = m.theme.TreeTable
	case NodeColumn:
		icon = "  "
		labelStyle = m.theme.TreeColumn
	}

	label := labelStyle.Render(n.Label)
	detail := ""
	if n.Detail != "" {
		detail = " " + m.theme.Dimmed.Render(fmt.Sprintf("(%s)", n.Detail))
	}

	line := indent + icon + label + detail

	if selected {
		line = m.theme.TreeSelected.Width(m.width - 2).Render(indent + icon + n.Label + detail)
	}

	return line
}

func (m Model) nodeDepth(n *Node) int {
	depth := 0
	p := n.parent
	for p != nil {
		depth++
		p = p.parent
	}
	return depth
}

// SelectedNode returns the currently highlighted node, or nil.
func (m Model) SelectedNode() *Node {
	if len(m.flat) == 0 {
		return nil
	}
	return m.flat[m.cursor]
}

// SetChildren replaces the children of the node matching connID+dbName (or connID+table).
// connID is always required; dbName is the parent database name (empty for connection-level).
// When called as SetChildren(connID, "", nodes) it sets database nodes under a connection.
// When called as SetChildren(connID, dbName, nodes) it sets table/view nodes under a database.
// When called as SetChildren(connID+":"+dbName, table, nodes) it sets column nodes under a table.
func (m *Model) SetChildren(connID, dbName string, children []*Node) {
	// Special case: connID may contain ":" for column-level (connID:dbName, table)
	for _, n := range m.flat {
		if n.ConnID == connID && n.DBName == dbName {
			n.Children = children
			for _, c := range children {
				c.parent = n
			}
			n.Expanded = true
			m.flatten()
			return
		}
	}
}

// ConsumeSelection returns and clears any pending selection from the last Enter press.
func (m *Model) ConsumeSelection() *Node {
	if m.pendingSel == nil {
		return nil
	}
	n := m.pendingSel
	m.pendingSel = nil
	return n
}

// NewTableNode creates a table node.
func NewTableNode(name, connID, dbName string) *Node {
	return &Node{Kind: NodeTable, Label: name, ConnID: connID, DBName: dbName}
}

// NewViewNode creates a view node.
func NewViewNode(name, connID, dbName string) *Node {
	return &Node{Kind: NodeView, Label: name, ConnID: connID, DBName: dbName}
}

// NewDatabaseNode creates a database node.
func NewDatabaseNode(name, connID string) *Node {
	return &Node{Kind: NodeDatabase, Label: name, ConnID: connID, DBName: name}
}

// NewColumnNode creates a column node.
func NewColumnNode(name, dataType, connID, dbName string) *Node {
	return &Node{Kind: NodeColumn, Label: name, Detail: dataType, ConnID: connID, DBName: dbName}
}
