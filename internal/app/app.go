package app

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/robertn/dbx/internal/config"
	"github.com/robertn/dbx/internal/db"
	"github.com/robertn/dbx/internal/history"
	"github.com/robertn/dbx/internal/ui/cmdpalette"
	"github.com/robertn/dbx/internal/ui/editor"
	"github.com/robertn/dbx/internal/ui/explorer"
	"github.com/robertn/dbx/internal/ui/results"
	"github.com/robertn/dbx/internal/ui/theme"
	"github.com/robertn/dbx/internal/util"
)

// Model is the root bubbletea application model.
type Model struct {
	cfg     *config.Config
	theme   theme.Theme
	keymap  KeyMap
	history *history.History

	width  int
	height int

	focus Panel

	explorerHidden bool
	fullscreenOn   bool
	fullscreenPanel Panel

	// Active connection state
	activeConnID string
	activeDB     string
	drivers      map[string]db.Driver // connID -> driver

	explorer explorer.Model
	editor   editor.Model
	results  results.Model
	palette  cmdpalette.Model
	connForm *explorer.ConnForm
	showForm bool
	showHelp bool

	spinner    spinner.Model
	isLoading  bool

	statusMsg     string
	statusExpiry  time.Time
}

// New creates the root application model.
func New(cfg *config.Config) Model {
	t := theme.Get(cfg.Theme)

	hist, _ := history.New() // ignore error; history is non-critical

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := Model{
		cfg:             cfg,
		theme:           t,
		keymap:          DefaultKeyMap(),
		history:         hist,
		focus:           PanelExplorer,
		fullscreenPanel: PanelExplorer,
		drivers:         make(map[string]db.Driver),
		explorer:        explorer.New(cfg, t),
		editor:          editor.New(t),
		results:         results.New(t),
		palette:         cmdpalette.New(t),
		spinner:         sp,
	}
	m.explorer.SetFocused(true)
	m.editor.SetFocused(false)
	m.results.SetFocused(false)
	return m
}

// clearStatusMsg is a delayed message to clear the status bar.
type clearStatusMsg struct{}

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layoutPanels()
		return m, nil

	case spinner.TickMsg:
		if m.isLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case clearStatusMsg:
		if time.Now().After(m.statusExpiry) {
			m.statusMsg = ""
		}
		return m, nil

	case explorer.ConnFormSubmitMsg:
		return m.handleConnFormSubmit(msg)

	case explorer.ConnFormCancelMsg:
		m.showForm = false
		m.connForm = nil
		return m, nil

	case tea.KeyMsg:
		// Route to connection form if active
		if m.showForm && m.connForm != nil {
			updated, cmd := m.connForm.Update(msg)
			m.connForm = &updated
			return m, cmd
		}

		if m.palette.IsVisible() {
			var cmd tea.Cmd
			m.palette, cmd = m.palette.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		// Global keys — only active when editor is NOT in insert mode
		editorInsert := m.editor.IsInsertMode()

		switch msg.String() {
		case "ctrl+c":
			m.closeAllDrivers()
			return m, tea.Quit

		case "e":
			if !editorInsert {
				m.setFocus(PanelExplorer)
				return m, nil
			}

		case "q":
			if !editorInsert && m.focus != PanelEditor {
				m.setFocus(PanelEditor)
				return m, nil
			}

		case "r":
			if !editorInsert {
				m.setFocus(PanelResults)
				return m, nil
			}

		case " ":
			if !editorInsert {
				m.openPalette()
				return m, nil
			}

		case "?":
			if !editorInsert {
				m.showHelp = !m.showHelp
				return m, nil
			}

		case "esc":
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
		}

	case cmdpalette.ExecuteCommandMsg:
		if msg.Action != nil {
			cmds = append(cmds, func() tea.Msg { return msg.Action() })
		}
		return m, tea.Batch(cmds...)

	case editor.ExecuteQueryMsg:
		if msg.Query == "" {
			return m, nil
		}
		m.isLoading = true
		m.results.SetLoading(true)
		if m.history != nil {
			key := m.connKey()
			_ = m.history.Add(key, msg.Query)
		}
		cmds = append(cmds, m.execQueryCmd(msg.Query))
		cmds = append(cmds, m.spinner.Tick)
		return m, tea.Batch(cmds...)

	case dbQueryResultMsg:
		m.isLoading = false
		qr := &results.QueryResult{
			Columns: msg.result.Columns,
			Rows:    msg.result.Rows,
			Error:   msg.result.Error,
			Elapsed: msg.elapsed,
		}
		m.results.SetResult(qr)
		if qr.Error != "" {
			m.setStatus("Query error: " + qr.Error)
		} else {
			m.setStatus("")
		}
		return m, nil

	case dbSchemaMsg:
		if msg.err != nil {
			m.statusMsg = "Schema error: " + msg.err.Error()
			return m, nil
		}
		nodes := make([]*explorer.Node, 0, len(msg.tables)+len(msg.views))
		for _, t := range msg.tables {
			nodes = append(nodes, explorer.NewTableNode(t, msg.connID, msg.dbName))
		}
		for _, v := range msg.views {
			nodes = append(nodes, explorer.NewViewNode(v, msg.connID, msg.dbName))
		}
		m.explorer.SetChildren(msg.connID, msg.dbName, nodes)
		// Update autocomplete with table names
		m.editor.SetSchema(msg.tables, nil)
		return m, nil

	case dbDatabasesMsg:
		if msg.err != nil {
			m.statusMsg = "DB list error: " + msg.err.Error()
			return m, nil
		}
		nodes := make([]*explorer.Node, 0, len(msg.databases))
		for _, dbName := range msg.databases {
			nodes = append(nodes, explorer.NewDatabaseNode(dbName, msg.connID))
		}
		m.explorer.SetChildren(msg.connID, "", nodes)
		return m, nil

	case dbColumnsMsg:
		if msg.err != nil {
			return m, nil
		}
		colNodes := make([]*explorer.Node, 0, len(msg.columns))
		for _, c := range msg.columns {
			colNodes = append(colNodes, explorer.NewColumnNode(c.Name, c.DataType, msg.connID, msg.dbName))
		}
		m.explorer.SetChildren(msg.connID+":"+msg.dbName, msg.table, colNodes)
		return m, nil

	case explorerSelectMsg:
		cmds = append(cmds, m.handleExplorerSelect(msg.node)...)
		return m, tea.Batch(cmds...)

	case toggleExplorerMsg:
		m.explorerHidden = !m.explorerHidden
		m.layoutPanels()
		return m, nil

	case toggleFullscreenMsg:
		m.fullscreenOn = !m.fullscreenOn
		if m.fullscreenOn {
			m.fullscreenPanel = m.focus
		}
		m.layoutPanels()
		return m, nil

	case addConnMsg:
		form := explorer.NewConnForm(m.theme)
		form.SetSize(m.width-4, m.height-4)
		m.connForm = &form
		m.showForm = true
		return m, nil

	case editConnMsg:
		if node := m.explorer.SelectedNode(); node != nil {
			conn := m.findConn(node.ConnID)
			if conn != nil {
				form := explorer.NewEditConnForm(m.theme, *conn)
				form.SetSize(m.width-4, m.height-4)
				m.connForm = &form
				m.showForm = true
			}
		}
		return m, nil

	case deleteConnMsg:
		if node := m.explorer.SelectedNode(); node != nil {
			for i, c := range m.cfg.Connections {
				if c.ID == node.ConnID {
					m.cfg.Connections = append(m.cfg.Connections[:i], m.cfg.Connections[i+1:]...)
					break
				}
			}
			_ = config.Save(m.cfg)
			m.explorer.SetConfig(m.cfg)
			m.statusMsg = "Connection deleted."
		}
		return m, nil

	case refreshSchemaMsg:
		if node := m.explorer.SelectedNode(); node != nil && node.DBName != "" {
			cmds = append(cmds, m.fetchSchemaCmd(node.ConnID, node.DBName))
		}
		return m, tea.Batch(cmds...)

	case execQueryFromPaletteMsg:
		query := m.editor.CurrentQuery()
		if query != "" {
			m.results.SetLoading(true)
			cmds = append(cmds, m.execQueryCmd(query))
		}
		return m, tea.Batch(cmds...)

	case clearEditorMsg:
		m.editor.SetContent("")
		return m, nil

	case copyCellMsg:
		cell := m.results.SelectedCell()
		if err := util.Copy(cell); err != nil {
			m.statusMsg = "Clipboard unavailable: " + err.Error()
		} else {
			m.statusMsg = "Cell copied to clipboard."
		}
		return m, nil

	case copyRowMsg:
		row := m.results.SelectedRow()
		if err := util.Copy(strings.Join(row, "\t")); err != nil {
			m.statusMsg = "Clipboard unavailable: " + err.Error()
		} else {
			m.statusMsg = "Row copied to clipboard."
		}
		return m, nil

	case exportCSVMsg:
		dir, _ := os.UserHomeDir()
		if r := m.results.Result(); r != nil {
			path, err := r.ExportCSV(dir)
			if err != nil {
				m.statusMsg = "Export error: " + err.Error()
			} else {
				m.statusMsg = "Exported to " + path
			}
		}
		return m, nil

	case exportJSONMsg:
		dir, _ := os.UserHomeDir()
		if r := m.results.Result(); r != nil {
			path, err := r.ExportJSON(dir)
			if err != nil {
				m.statusMsg = "Export error: " + err.Error()
			} else {
				m.statusMsg = "Exported to " + path
			}
		}
		return m, nil
	}

	// Route key/other events to focused panel
	switch m.focus {
	case PanelExplorer:
		var cmd tea.Cmd
		m.explorer, cmd = m.explorer.Update(msg)
		cmds = append(cmds, cmd)
		// Check if explorer triggered a selection
		if sel := m.explorer.ConsumeSelection(); sel != nil {
			cmds = append(cmds, func() tea.Msg { return explorerSelectMsg{node: sel} })
		}
	case PanelEditor:
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		cmds = append(cmds, cmd)
	case PanelResults:
		var cmd tea.Cmd
		m.results, cmd = m.results.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) setFocus(p Panel) {
	m.focus = p
	m.explorer.SetFocused(p == PanelExplorer)
	m.editor.SetFocused(p == PanelEditor)
	m.results.SetFocused(p == PanelResults)
}

func (m *Model) connKey() string {
	if m.activeConnID == "" {
		return ""
	}
	return m.activeConnID + ":" + m.activeDB
}

func (m *Model) openPalette() {
	var title string
	var commands []cmdpalette.Command

	switch m.focus {
	case PanelExplorer:
		title = "Explorer Commands"
		commands = []cmdpalette.Command{
			{Key: "a", Description: "Add connection", Action: func() tea.Msg { return addConnMsg{} }},
			{Key: "e", Description: "Edit connection", Action: func() tea.Msg { return editConnMsg{} }},
			{Key: "d", Description: "Delete connection", Action: func() tea.Msg { return deleteConnMsg{} }},
			{Key: "R", Description: "Refresh schema", Action: func() tea.Msg { return refreshSchemaMsg{} }},
			{Key: "t", Description: "Toggle explorer", Action: func() tea.Msg { return toggleExplorerMsg{} }},
			{Key: "f", Description: "Fullscreen", Action: func() tea.Msg { return toggleFullscreenMsg{} }},
		}
	case PanelEditor:
		title = "Editor Commands"
		commands = []cmdpalette.Command{
			{Key: "x", Description: "Execute query (ctrl+enter)", Action: func() tea.Msg { return execQueryFromPaletteMsg{} }},
			{Key: "c", Description: "Clear editor", Action: func() tea.Msg { return clearEditorMsg{} }},
			{Key: "f", Description: "Fullscreen", Action: func() tea.Msg { return toggleFullscreenMsg{} }},
		}
	case PanelResults:
		title = "Results Commands"
		commands = []cmdpalette.Command{
			{Key: "y", Description: "Copy cell", Action: func() tea.Msg { return copyCellMsg{} }},
			{Key: "Y", Description: "Copy row", Action: func() tea.Msg { return copyRowMsg{} }},
			{Key: "e", Description: "Export CSV", Action: func() tea.Msg { return exportCSVMsg{} }},
			{Key: "j", Description: "Export JSON", Action: func() tea.Msg { return exportJSONMsg{} }},
			{Key: "f", Description: "Fullscreen", Action: func() tea.Msg { return toggleFullscreenMsg{} }},
		}
	}

	m.palette.Show(title, commands)
}

func (m *Model) handleExplorerSelect(node *explorer.Node) []tea.Cmd {
	if node == nil {
		return nil
	}
	var cmds []tea.Cmd

	switch node.Kind {
	case explorer.NodeConnection:
		cmds = append(cmds, m.connectCmd(node.ConnID))

	case explorer.NodeDatabase:
		m.activeConnID = node.ConnID
		m.activeDB = node.DBName
		m.editor.SwitchConnection(m.connKey())
		// Load history for this connection
		if m.history != nil {
			entries := m.history.ForKey(m.connKey())
			queries := make([]string, len(entries))
			for i, e := range entries {
				queries[i] = e.Query
			}
			m.editor.SetHistory(queries)
		}
		cmds = append(cmds, m.fetchSchemaCmd(node.ConnID, node.DBName))

	case explorer.NodeTable, explorer.NodeView:
		if node.Detail == "select" {
			driver := ""
			if conn := m.findConn(node.ConnID); conn != nil {
				driver = conn.Driver
			}
			query := quickSelectQuery(driver, node.DBName, node.Label)
			m.editor.SetContent(query)
			m.setFocus(PanelResults)
			m.results.SetLoading(true)
			cmds = append(cmds, m.execQueryCmd(query))
		} else {
			cmds = append(cmds, m.fetchColumnsCmd(node.ConnID, node.DBName, node.Label))
		}

	case explorer.NodeColumn:
		// Nothing to do
	}

	return cmds
}

func (m *Model) connectCmd(connID string) tea.Cmd {
	conn := m.findConn(connID)
	if conn == nil {
		return nil
	}
	return func() tea.Msg {
		// If connection has database(s) configured, use those instead of fetching all
		if conn.Database != "" {
			parts := strings.Split(conn.Database, ",")
			dbs := make([]string, 0, len(parts))
			for _, p := range parts {
				if name := strings.TrimSpace(p); name != "" {
					dbs = append(dbs, name)
				}
			}
			if len(dbs) > 0 {
				return dbDatabasesMsg{connID: connID, databases: dbs, err: nil}
			}
		}

		driver, err := db.New(*conn)
		if err != nil {
			return dbDatabasesMsg{connID: connID, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := driver.Connect(ctx, *conn); err != nil {
			return dbDatabasesMsg{connID: connID, err: err}
		}
		dbs, err := driver.Databases(ctx)
		return dbDatabasesMsg{connID: connID, databases: dbs, err: err}
	}
}

func (m *Model) fetchSchemaCmd(connID, dbName string) tea.Cmd {
	conn := m.findConn(connID)
	if conn == nil {
		return nil
	}
	return func() tea.Msg {
		driver, err := db.New(*conn)
		if err != nil {
			return dbSchemaMsg{connID: connID, dbName: dbName, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := driver.Connect(ctx, *conn); err != nil {
			return dbSchemaMsg{connID: connID, dbName: dbName, err: err}
		}
		defer driver.Close()
		tables, err := driver.Tables(ctx, dbName)
		if err != nil {
			return dbSchemaMsg{connID: connID, dbName: dbName, err: err}
		}
		views, _ := driver.Views(ctx, dbName)
		return dbSchemaMsg{connID: connID, dbName: dbName, tables: tables, views: views}
	}
}

func (m *Model) fetchColumnsCmd(connID, dbName, table string) tea.Cmd {
	conn := m.findConn(connID)
	if conn == nil {
		return nil
	}
	return func() tea.Msg {
		driver, err := db.New(*conn)
		if err != nil {
			return dbColumnsMsg{connID: connID, dbName: dbName, table: table, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := driver.Connect(ctx, *conn); err != nil {
			return dbColumnsMsg{connID: connID, dbName: dbName, table: table, err: err}
		}
		defer driver.Close()
		cols, err := driver.Columns(ctx, dbName, table)
		return dbColumnsMsg{connID: connID, dbName: dbName, table: table, columns: cols, err: err}
	}
}

func (m *Model) execQueryCmd(query string) tea.Cmd {
	connID := m.activeConnID
	dbName := m.activeDB
	conn := m.findConn(connID)
	return func() tea.Msg {
		start := time.Now()
		if conn == nil {
			return dbQueryResultMsg{
				result:  &db.QueryResult{Error: "No active connection. Select a database in the explorer first."},
				elapsed: 0,
			}
		}
		driver, err := db.New(*conn)
		if err != nil {
			return dbQueryResultMsg{result: &db.QueryResult{Error: err.Error()}}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := driver.Connect(ctx, *conn); err != nil {
			return dbQueryResultMsg{result: &db.QueryResult{Error: err.Error()}}
		}
		defer driver.Close()

		// Determine if SELECT or DML
		trimmed := strings.TrimSpace(strings.ToUpper(query))
		var result *db.QueryResult
		if strings.HasPrefix(trimmed, "SELECT") || strings.HasPrefix(trimmed, "WITH") ||
			strings.HasPrefix(trimmed, "SHOW") || strings.HasPrefix(trimmed, "EXPLAIN") ||
			strings.HasPrefix(trimmed, "DESCRIBE") || strings.HasPrefix(trimmed, "DESC") {
			result, err = driver.Query(ctx, dbName, query)
		} else {
			result, err = driver.Exec(ctx, dbName, query)
		}
		if err != nil {
			result = &db.QueryResult{Error: err.Error()}
		}
		return dbQueryResultMsg{result: result, elapsed: time.Since(start)}
	}
}

// quickSelectQuery returns a driver-appropriate SELECT 100 query.
func quickSelectQuery(driver, database, table string) string {
	switch driver {
	case "mssql", "sqlserver":
		if database != "" {
			return "SELECT TOP 100 * FROM [" + database + "].[dbo].[" + table + "]"
		}
		return "SELECT TOP 100 * FROM [dbo].[" + table + "]"
	case "mysql":
		if database != "" {
			return "SELECT * FROM `" + database + "`.`" + table + "` LIMIT 100"
		}
		return "SELECT * FROM `" + table + "` LIMIT 100"
	case "sqlite", "sqlite3":
		return "SELECT * FROM \"" + table + "\" LIMIT 100"
	default: // postgres and anything else
		return "SELECT * FROM " + table + " LIMIT 100"
	}
}

func (m *Model) setStatus(msg string) {
	m.statusMsg = msg
	if msg != "" {
		m.statusExpiry = time.Now().Add(4 * time.Second)
	}
}

func (m *Model) handleConnFormSubmit(msg explorer.ConnFormSubmitMsg) (tea.Model, tea.Cmd) {
	conn := msg.Conn
	if msg.IsEdit {
		for i, c := range m.cfg.Connections {
			if c.ID == conn.ID {
				m.cfg.Connections[i] = conn
				break
			}
		}
	} else {
		m.cfg.Connections = append(m.cfg.Connections, conn)
	}
	if err := config.Save(m.cfg); err != nil {
		m.statusMsg = "Error saving config: " + err.Error()
	} else {
		m.statusMsg = "Connection saved."
	}
	m.showForm = false
	m.connForm = nil
	m.explorer.SetConfig(m.cfg)
	return m, nil
}

func (m *Model) findConn(connID string) *config.Connection {
	for i, c := range m.cfg.Connections {
		if c.ID == connID {
			return &m.cfg.Connections[i]
		}
	}
	return nil
}

func (m *Model) closeAllDrivers() {
	for _, d := range m.drivers {
		_ = d.Close()
	}
}

func (m *Model) layoutPanels() {
	if m.width == 0 || m.height == 0 {
		return
	}

	totalH := m.height - 1

	if m.fullscreenOn {
		m.explorer.SetSize(m.width-2, totalH-2)
		m.editor.SetSize(m.width-2, totalH-2)
		m.results.SetSize(m.width-2, totalH-2)
		return
	}

	explorerW := m.width * m.cfg.Layout.ExplorerWidthPct / 100
	if m.explorerHidden {
		explorerW = 0
	}
	rightW := m.width - explorerW

	editorH := totalH * m.cfg.Layout.EditorHeightPct / 100
	resultsH := totalH - editorH

	m.explorer.SetSize(explorerW-2, totalH-2)
	m.editor.SetSize(rightW-2, editorH-2)
	m.results.SetSize(rightW-2, resultsH-2)
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.fullscreenOn {
		content := m.renderPanelContent(m.fullscreenPanel)
		border := m.theme.BorderFocused.Width(m.width - 2).Height(m.height - 3).Render(content)
		return lipgloss.JoinVertical(lipgloss.Left, border, m.renderStatusBar())
	}

	explorerView := ""
	if !m.explorerHidden {
		explorerView = m.renderBorderedPanel(PanelExplorer)
	}

	editorView := m.renderBorderedPanel(PanelEditor)
	resultsView := m.renderBorderedPanel(PanelResults)

	rightPane := lipgloss.JoinVertical(lipgloss.Left, editorView, resultsView)

	var mainView string
	if m.explorerHidden {
		mainView = rightPane
	} else {
		mainView = lipgloss.JoinHorizontal(lipgloss.Top, explorerView, rightPane)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, mainView, m.renderStatusBar())

	if m.palette.IsVisible() {
		paletteView := m.palette.View()
		view = overlayBottomRight(view, paletteView, m.width, m.height)
	}

	if m.showForm && m.connForm != nil {
		formView := m.theme.BorderFocused.Width(m.width - 6).Height(m.height - 6).Render(m.connForm.View())
		view = overlayCentered(view, formView, m.width, m.height)
	}

	if m.showHelp {
		helpView := m.renderHelp()
		view = overlayCentered(view, helpView, m.width, m.height)
	}

	return view
}

func (m Model) renderPanelContent(p Panel) string {
	switch p {
	case PanelExplorer:
		return m.explorer.View()
	case PanelEditor:
		return m.editor.View()
	case PanelResults:
		return m.results.View()
	}
	return ""
}

func (m Model) renderBorderedPanel(p Panel) string {
	focused := m.focus == p
	content := m.renderPanelContent(p)

	var borderStyle lipgloss.Style
	if focused {
		borderStyle = m.theme.BorderFocused
	} else {
		borderStyle = m.theme.BorderUnfocused
	}

	totalH := m.height - 1
	switch p {
	case PanelExplorer:
		w := m.width*m.cfg.Layout.ExplorerWidthPct/100 - 2
		h := totalH - 2
		if w < 0 {
			w = 0
		}
		return borderStyle.Width(w).Height(h).Render(content)
	case PanelEditor:
		w := m.width - m.width*m.cfg.Layout.ExplorerWidthPct/100 - 2
		h := totalH*m.cfg.Layout.EditorHeightPct/100 - 2
		if w < 0 {
			w = 0
		}
		return borderStyle.Width(w).Height(h).Render(content)
	case PanelResults:
		w := m.width - m.width*m.cfg.Layout.ExplorerWidthPct/100 - 2
		h := totalH*(100-m.cfg.Layout.EditorHeightPct)/100 - 2
		if w < 0 {
			w = 0
		}
		return borderStyle.Width(w).Height(h).Render(content)
	}
	return content
}

func (m Model) renderStatusBar() string {
	connInfo := "no connection"
	if m.activeConnID != "" {
		connInfo = m.activeConnID
		if m.activeDB != "" {
			connInfo += "/" + m.activeDB
		}
	}

	var status string
	if m.isLoading {
		status = m.spinner.View() + " Running query..."
	} else if m.statusMsg != "" {
		status = m.statusMsg
	} else {
		status = "  [" + m.focus.String() + "]  " + connInfo + "  space: commands  e/q/r: focus  ?: help  ctrl+c: quit"
	}
	return m.theme.StatusBar.Width(m.width).Render(status)
}

func (m Model) renderHelp() string {
	help := `
  dbx — TUI Database Client

  NAVIGATION
    e           Focus explorer
    q           Focus editor
    r           Focus results
    space       Open command palette (context-aware)
    space+f     Toggle fullscreen for current panel
    ?           Toggle this help

  EXPLORER
    j/k         Navigate up/down
    enter/l     Expand/collapse node
    s           Quick SELECT * FROM table LIMIT 100

  EDITOR (Normal mode)
    i/a/o       Enter insert mode
    ctrl+enter  Execute query under cursor
    ctrl+p/n    Browse query history
    dd          Delete line
    gg/G        Go to top/bottom

  EDITOR (Insert mode)
    esc         Return to normal mode
    tab         Trigger autocomplete
    ctrl+enter  Execute query

  RESULTS
    j/k         Navigate rows
    h/l         Navigate columns
    g/G         First/last row

  COMMAND PALETTE (space)
    Explorer:   a=add  e=edit  d=delete  R=refresh  t=toggle  f=fullscreen
    Editor:     x=execute  c=clear  f=fullscreen
    Results:    y=copy cell  Y=copy row  e=export CSV  j=export JSON  f=fullscreen

  Press ? or esc to close
`
	return m.theme.BorderFocused.Width(m.width - 6).Render(
		lipgloss.NewStyle().Padding(1, 2).Render(strings.TrimLeft(help, "\n")),
	)
}

func overlayCentered(base, overlay string, width, height int) string {
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")

	overlayH := len(overlayLines)
	overlayW := 0
	for _, l := range overlayLines {
		if lw := lipgloss.Width(l); lw > overlayW {
			overlayW = lw
		}
	}

	startRow := (height - overlayH) / 2
	startCol := (width - overlayW) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	for i, ol := range overlayLines {
		row := startRow + i
		if row >= len(baseLines) {
			break
		}
		line := []rune(baseLines[row])
		for len(line) < startCol+lipgloss.Width(ol) {
			line = append(line, ' ')
		}
		olRunes := []rune(ol)
		for j, r := range olRunes {
			pos := startCol + j
			if pos < len(line) {
				line[pos] = r
			}
		}
		baseLines[row] = string(line)
	}
	return strings.Join(baseLines, "\n")
}

func overlayBottomRight(base, overlay string, width, height int) string {
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")

	overlayW := 0
	for _, l := range overlayLines {
		if lw := lipgloss.Width(l); lw > overlayW {
			overlayW = lw
		}
	}

	startRow := height - len(overlayLines) - 2
	if startRow < 0 {
		startRow = 0
	}
	startCol := width - overlayW - 1
	if startCol < 0 {
		startCol = 0
	}

	for i, ol := range overlayLines {
		row := startRow + i
		if row >= len(baseLines) {
			break
		}
		line := []rune(baseLines[row])
		for len(line) < startCol+lipgloss.Width(ol) {
			line = append(line, ' ')
		}
		olRunes := []rune(ol)
		for j, r := range olRunes {
			pos := startCol + j
			if pos < len(line) {
				line[pos] = r
			}
		}
		baseLines[row] = string(line)
	}

	return strings.Join(baseLines, "\n")
}
