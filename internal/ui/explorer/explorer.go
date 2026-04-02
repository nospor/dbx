package explorer

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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

// Rows of context kept below the cursor when scrolling (cursor stays off the bottom edge).
const explorerScrollMargin = 10

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

	filterInput textinput.Model
	filtering   bool
	filterText  string
}

// New creates a new explorer model.
func New(cfg *config.Config, t theme.Theme) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "Filter tables..."
	ti.CharLimit = 100

	m := Model{
		cfg:         cfg,
		theme:       t,
		filterInput: ti,
	}
	m.buildTree()
	return m
}

func (m *Model) buildTree() {
	conns := append([]config.Connection(nil), m.cfg.Connections...)
	sort.Slice(conns, func(i, j int) bool {
		return strings.ToLower(conns[i].Name) < strings.ToLower(conns[j].Name)
	})
	m.nodes = make([]*Node, 0, len(conns))
	for _, conn := range conns {
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
	if m.filterText != "" && (n.Kind == NodeTable || n.Kind == NodeView || n.Kind == NodeColumn) {
		if !strings.Contains(strings.ToLower(n.Label), m.filterText) {
			return
		}
	}

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

// IsFiltering returns true if the user is currently typing in the filter prompt.
func (m Model) IsFiltering() bool {
	return m.filtering
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	var cmd tea.Cmd

	if m.filtering {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter", "esc":
				m.filtering = false
				m.filterInput.Blur()
				return m, nil
			}
		}

		m.filterInput, cmd = m.filterInput.Update(msg)
		newFilterText := strings.ToLower(m.filterInput.Value())
		if newFilterText != m.filterText {
			m.filterText = newFilterText
			m.flatten()
			if m.cursor >= len(m.flat) {
				m.cursor = max(0, len(m.flat)-1)
			}
		}
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "f":
			m.filtering = true
			m.filterInput.Focus()
			return m, textinput.Blink
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
				if len(n.Children) > 0 || n.Kind == NodeConnection || n.Kind == NodeDatabase || n.Kind == NodeTable {
					n.Expanded = !n.Expanded
					m.flatten()
				}
				m.pendingSel = n
			}
		case "h":
			if len(m.flat) > 0 {
				n := m.flat[m.cursor]
				target := n
				// If cursor is inside a subtree leaf/item, collapse the nearest expanded parent.
				if !target.Expanded && target.parent != nil {
					target = target.parent
				}
				if target.Expanded {
					target.Expanded = false
					m.flatten()
				}
				// Keep cursor on the collapsed node for predictable navigation.
				for i, fn := range m.flat {
					if fn == target {
						m.cursor = i
						break
					}
				}
				m.pendingSel = target
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
		case "v":
			// Table/view DDL popup (handled in app)
			if len(m.flat) > 0 {
				n := m.flat[m.cursor]
				if n.Kind == NodeTable || n.Kind == NodeView {
					m.pendingSel = &Node{
						Kind:   n.Kind,
						Label:  n.Label,
						ConnID: n.ConnID,
						DBName: n.DBName,
						Detail: "ddl",
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

// explorerScrollStart is the flat index of the first visible tree row.
// Keeps the cursor explorerScrollMargin rows above the bottom of the viewport when possible
// so items below the selection stay visible; at the end of the list the window clamps.
func explorerScrollStart(cursor, treeLines, flatLen int) int {
	if treeLines <= 0 || flatLen <= 0 {
		return 0
	}
	margin := explorerScrollMargin
	if margin > treeLines-2 {
		margin = max(0, treeLines-2)
	}
	maxRowFromTop := treeLines - 1 - margin
	if maxRowFromTop < 0 {
		maxRowFromTop = 0
	}
	start := cursor - maxRowFromTop
	if start < 0 {
		start = 0
	}
	if start+treeLines > flatLen {
		start = flatLen - treeLines
		if start < 0 {
			start = 0
		}
	}
	return start
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	var sb strings.Builder

	if len(m.flat) == 0 && m.filterText == "" {
		hint := m.theme.Dimmed.Render("No connections.\nPress space → add connection")
		return lipgloss.NewStyle().Width(m.width).Height(m.height).Render(hint)
	}

	// Match previous inner line budget: was 1 title + (height-2) tree rows (= height-1 rows).
	// Border title moved outside; keep total inner lines at height-1 so the panel doesn't overflow.
	visibleLines := m.height - 1
	treeLines := visibleLines
	if m.filtering || m.filterText != "" {
		treeLines-- // reserve line for the prompt
	}

	if treeLines < 0 {
		treeLines = 0
	}
	start := explorerScrollStart(m.cursor, treeLines, len(m.flat))

	linesRendered := 0
	for i := start; i < len(m.flat) && i < start+treeLines; i++ {
		if linesRendered > 0 {
			sb.WriteByte('\n')
		}
		n := m.flat[i]
		sb.WriteString(m.renderNode(n, i == m.cursor))
		linesRendered++
	}

	for linesRendered < treeLines {
		sb.WriteByte('\n')
		linesRendered++
	}

	if m.filtering || m.filterText != "" {
		if linesRendered > 0 {
			sb.WriteByte('\n')
		}
		if m.filtering {
			sb.WriteString(m.filterInput.View())
		} else {
			prompt := m.theme.Dimmed.Render("Filter: ") + m.theme.TreeTable.Render(m.filterText)
			sb.WriteString(prompt)
		}
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
			icon = "▶ "
		}
		labelStyle = m.theme.TreeTable
	case NodeView:
		icon = "  "
		labelStyle = m.theme.TreeTable
	case NodeColumn:
		icon = "  "
		labelStyle = m.theme.TreeColumn
	}

	prefix := indent + icon
	suffixPlain := ""
	if n.Detail != "" {
		suffixPlain = " (" + n.Detail + ")"
	}
	maxW := m.width
	prefixW := ansi.StringWidth(prefix)
	suffixW := ansi.StringWidth(suffixPlain)
	labBudget := maxW - prefixW - suffixW
	if suffixPlain != "" && labBudget < 1 {
		suffixBudget := max(1, maxW-prefixW-3)
		if suffixBudget >= 2 {
			suffixPlain = " (" + ansi.Truncate(n.Detail, max(1, suffixBudget-3), "…") + ")"
			suffixW = ansi.StringWidth(suffixPlain)
		} else {
			suffixPlain = ""
			suffixW = 0
		}
		labBudget = maxW - prefixW - suffixW
	}
	if labBudget < 1 {
		labBudget = 1
	}
	tlab := ansi.Truncate(n.Label, labBudget, "…")

	if selected {
		fullPlain := prefix + n.Label + suffixPlain
		trunc := ansi.Truncate(fullPlain, maxW, "…")
		return m.theme.TreeSelected.Render(trunc)
	}

	detailRendered := ""
	if suffixPlain != "" {
		detailRendered = m.theme.Dimmed.Render(suffixPlain)
	}
	return prefix + labelStyle.Render(tlab) + detailRendered
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

// SetChildren replaces children for connection, database, or table/view nodes.
// connID is always required; dbName is the parent database name (empty for connection-level).
// When called as SetChildren(connID, "", nodes) it sets database nodes under a connection.
// When called as SetChildren(connID, dbName, nodes) it sets table/view nodes under a database.
// When called as SetChildren(connID+":"+dbName, table, nodes) it sets column nodes under a table.
func (m *Model) SetChildren(connID, dbName string, children []*Node) {
	// Special case: column-level lookup.
	// Caller passes connID as "connID:dbName" and dbName as table/view name.
	if strings.Contains(connID, ":") && dbName != "" {
		parts := strings.SplitN(connID, ":", 2)
		if len(parts) == 2 {
			cid := parts[0]
			db := parts[1]
			table := dbName
			for _, n := range m.flat {
				if n.ConnID == cid && n.DBName == db && n.Label == table && (n.Kind == NodeTable || n.Kind == NodeView) {
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
	}

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

// CollapseConnection folds a connection node and all descendants (used by app when switching tabs).
func (m *Model) CollapseConnection(connID string) {
	if connID == "" {
		return
	}
	for _, n := range m.nodes {
		if n.ConnID == connID && n.Kind == NodeConnection {
			collapseSubtree(n)
			m.flatten()
			return
		}
	}
}

// CollapseDatabaseSubtree folds one database branch under a connection (tables/views/columns).
func (m *Model) CollapseDatabaseSubtree(connID, dbName string) {
	if connID == "" || dbName == "" {
		return
	}
	var conn *Node
	for _, n := range m.nodes {
		if n.ConnID == connID && n.Kind == NodeConnection {
			conn = n
			break
		}
	}
	if conn == nil {
		return
	}
	for _, c := range conn.Children {
		if c.Kind == NodeDatabase && c.DBName == dbName {
			collapseSubtree(c)
			m.flatten()
			return
		}
	}
}

func collapseSubtree(n *Node) {
	n.Expanded = false
	for _, c := range n.Children {
		collapseSubtree(c)
	}
}

// SelectDatabaseNode expands the path to connID and dbName and moves the cursor there.
// Returns false if the connection or database node is not present yet (e.g. databases not loaded).
func (m *Model) SelectDatabaseNode(connID, dbName string) bool {
	if connID == "" || dbName == "" {
		return false
	}
	var conn *Node
	for _, n := range m.nodes {
		if n.ConnID == connID && n.Kind == NodeConnection {
			conn = n
			break
		}
	}
	if conn == nil {
		return false
	}
	conn.Expanded = true
	var dbNode *Node
	for _, c := range conn.Children {
		if c.Kind == NodeDatabase && c.DBName == dbName {
			dbNode = c
			break
		}
	}
	if dbNode == nil {
		m.flatten()
		for i, fn := range m.flat {
			if fn == conn {
				m.cursor = i
				break
			}
		}
		return false
	}
	dbNode.Expanded = true
	m.flatten()
	for i, fn := range m.flat {
		if fn == dbNode {
			m.cursor = i
			return true
		}
	}
	return false
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
