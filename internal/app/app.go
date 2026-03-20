package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/robertn/dbx/internal/config"
	"github.com/robertn/dbx/internal/db"
	"github.com/robertn/dbx/internal/history"
	"github.com/robertn/dbx/internal/querycontents"
	"github.com/robertn/dbx/internal/sqlutil"
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

	queryContents *querycontents.Store

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
	schemaTables map[string][]string  // connID:db -> tables/views
	schemaCols   map[string][]string  // connID:db -> column tokens
	tableCols    map[string][]db.ColumnInfo // connID:db:table -> columns (cache + explorer)

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

func newEditorWithDrafts(t theme.Theme, drafts map[string]string) editor.Model {
	ed := editor.New(t)
	ed.SeedQueryTabs(drafts)
	return ed
}

// New creates the root application model.
func New(cfg *config.Config) Model {
	t := theme.Get(cfg.Theme)

	hist := history.NewOrEmpty()
	qc := querycontents.NewOrEmpty()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := Model{
		cfg:             cfg,
		theme:           t,
		keymap:          DefaultKeyMap(),
		history:         hist,
		queryContents:   qc,
		focus:           PanelExplorer,
		fullscreenPanel: PanelExplorer,
		drivers:         make(map[string]db.Driver),
		schemaTables:    make(map[string][]string),
		schemaCols:      make(map[string][]string),
		tableCols:       make(map[string][]db.ColumnInfo),
		explorer:        explorer.New(cfg, t),
		editor:          newEditorWithDrafts(t, qc.All()),
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
type clearStatusMsg struct {
	expiresAt time.Time
}

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
		if !msg.expiresAt.IsZero() && msg.expiresAt.Equal(m.statusExpiry) && time.Now().After(m.statusExpiry) {
			m.statusMsg = ""
		}
		return m, nil

	case results.DeleteDraftMsg:
		if msg.Err != "" {
			return m, m.setStatus(msg.Err)
		}
		m.editor.AppendAtEnd(msg.SQL)
		m.persistEditorDraft()
		m.setFocus(PanelEditor)
		return m, m.setStatus("DELETE draft appended — review before running.")

	case results.DeleteDraftRequestMsg:
		return m, m.buildDeleteDraftCmd(msg)

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
			m.persistEditorDraft()
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
		key := m.connKey()
		if key == "" {
			key = "_"
		}
		if m.history != nil {
			if err := m.history.Add(key, msg.Query); err != nil {
				cmds = append(cmds, m.setStatus("History save failed: "+err.Error()))
			} else {
				cmds = append(cmds, m.setStatus("Saved to history"))
				// Refresh the editor's in-memory history so the popup shows the latest
				entries := m.history.ForKey(key)
				queries := make([]string, len(entries))
				for i, e := range entries {
					queries[i] = e.Query
				}
				m.editor.ReplaceHistoryEntries(queries)
			}
		}
		cmds = append(cmds, m.execQueryCmd(msg.Query))
		cmds = append(cmds, m.spinner.Tick)
		return m, tea.Batch(cmds...)

	case editor.QueryPanePersistMsg:
		if m.queryContents != nil {
			key := msg.ConnKey
			if key == "" {
				key = "_"
			}
			if err := m.queryContents.Put(key, msg.Text); err != nil {
				cmds = append(cmds, m.setStatus("Query draft save failed: "+err.Error()))
			}
		}
		return m, tea.Batch(cmds...)

	case editor.DeleteHistoryEntryMsg:
		if m.history == nil || msg.Query == "" {
			return m, nil
		}
		key := m.connKey()
		if key == "" {
			key = "_"
		}
		if err := m.history.Remove(key, msg.Query); err != nil {
			cmds = append(cmds, m.setStatus("History delete failed: "+err.Error()))
		} else {
			cmds = append(cmds, m.setStatus("Removed from history"))
		}
		entries := m.history.ForKey(key)
		queries := make([]string, len(entries))
		for i, e := range entries {
			queries[i] = e.Query
		}
		m.editor.ReplaceHistoryEntries(queries)
		return m, tea.Batch(cmds...)

	case dbQueryResultMsg:
		m.isLoading = false
		drv := ""
		if c := m.findConn(m.activeConnID); c != nil {
			drv = c.Driver
		}
		qr := &results.QueryResult{
			Columns:   msg.result.Columns,
			Rows:      msg.result.Rows,
			Error:     msg.result.Error,
			Elapsed:   msg.elapsed,
			SourceSQL: msg.sourceSQL,
			Driver:    drv,
			Database:  m.activeDB,
		}
		m.results.SetResult(qr)
		if qr.Error != "" {
			cmds = append(cmds, m.setStatus("Query error: "+qr.Error))
		} else {
			cmds = append(cmds, m.setStatus(""))
		}
		return m, tea.Batch(cmds...)

	case dbSchemaMsg:
		if msg.err != nil {
			cmds = append(cmds, m.setStatus("Schema error: "+msg.err.Error()))
			return m, tea.Batch(cmds...)
		}
		nodes := make([]*explorer.Node, 0, len(msg.tables)+len(msg.views))
		for _, t := range msg.tables {
			nodes = append(nodes, explorer.NewTableNode(t, msg.connID, msg.dbName))
		}
		for _, v := range msg.views {
			nodes = append(nodes, explorer.NewViewNode(v, msg.connID, msg.dbName))
		}
		m.explorer.SetChildren(msg.connID, msg.dbName, nodes)
		key := schemaKey(msg.connID, msg.dbName)
		tokens := make([]string, 0, len(msg.tables)+len(msg.views))
		tokens = append(tokens, msg.tables...)
		tokens = append(tokens, msg.views...)
		m.schemaTables[key] = tokens
		m.clearTableColumnCache(msg.connID, msg.dbName)
		m.schemaCols[key] = nil
		if len(msg.allColumns) > 0 {
			m.ingestBulkColumns(msg.connID, msg.dbName, msg.allColumns)
		} else if msg.connID == m.activeConnID && msg.dbName == m.activeDB {
			m.applyEditorSchema(msg.connID, msg.dbName)
		}
		cmds = append(cmds, m.setStatus(""))
		return m, tea.Batch(cmds...)

	case dbDatabasesMsg:
		if msg.err != nil {
			cmds = append(cmds, m.setStatus("DB list error: "+msg.err.Error()))
			return m, tea.Batch(cmds...)
		}
		nodes := make([]*explorer.Node, 0, len(msg.databases))
		for _, dbName := range msg.databases {
			nodes = append(nodes, explorer.NewDatabaseNode(dbName, msg.connID))
		}
		m.explorer.SetChildren(msg.connID, "", nodes)
		// Auto-select when connection has exactly one database (loads history, fetches schema)
		if len(msg.databases) == 1 {
			cmds = append(cmds, func() tea.Msg {
				return explorerSelectMsg{node: explorer.NewDatabaseNode(msg.databases[0], msg.connID)}
			})
		}
		cmds = append(cmds, m.setStatus(""))
		return m, tea.Batch(cmds...)

	case dbColumnsMsg:
		if msg.err != nil {
			cmds = append(cmds, m.setStatus("Columns error: "+msg.err.Error()))
			return m, tea.Batch(cmds...)
		}
		colNodes := make([]*explorer.Node, 0, len(msg.columns))
		for _, c := range msg.columns {
			colNodes = append(colNodes, explorer.NewColumnNode(c.Name, c.DataType, msg.connID, msg.dbName))
		}
		m.explorer.SetChildren(msg.connID+":"+msg.dbName, msg.table, colNodes)
		ck := tableColsKey(msg.connID, msg.dbName, msg.table)
		m.tableCols[ck] = append([]db.ColumnInfo(nil), msg.columns...)
		key := schemaKey(msg.connID, msg.dbName)
		existing := m.schemaCols[key]
		seen := make(map[string]struct{}, len(existing)+len(msg.columns)*2)
		for _, v := range existing {
			seen[v] = struct{}{}
		}
		for _, c := range msg.columns {
			if c.Name == "" {
				continue
			}
			if _, ok := seen[c.Name]; !ok {
				existing = append(existing, c.Name)
				seen[c.Name] = struct{}{}
			}
			qualified := msg.table + "." + c.Name
			if _, ok := seen[qualified]; !ok {
				existing = append(existing, qualified)
				seen[qualified] = struct{}{}
			}
		}
		sort.Strings(existing)
		m.schemaCols[key] = existing
		if msg.connID == m.activeConnID && msg.dbName == m.activeDB {
			m.applyEditorSchema(msg.connID, msg.dbName)
		}
		cmds = append(cmds, m.setStatus(""))
		return m, tea.Batch(cmds...)

	case explorerSelectMsg:
		cmds = append(cmds, m.handleExplorerSelect(msg.node)...)
		return m, tea.Batch(cmds...)

	case toggleExplorerMsg:
		m.explorerHidden = !m.explorerHidden
		if m.explorerHidden && m.focus == PanelExplorer {
			m.setFocus(PanelEditor)
		}
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
			cmds = append(cmds, m.setStatus("Connection deleted."))
		}
		return m, tea.Batch(cmds...)

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
		m.persistEditorDraft()
		return m, nil

	case copyCellMsg:
		cell := m.results.SelectedCell()
		if err := util.Copy(cell); err != nil {
			cmds = append(cmds, m.setStatus("Clipboard unavailable: "+err.Error()))
		} else {
			cmds = append(cmds, m.setStatus("Cell copied to clipboard."))
		}
		return m, tea.Batch(cmds...)

	case copyRowMsg:
		row := m.results.SelectedRow()
		if err := util.Copy(strings.Join(row, "\t")); err != nil {
			cmds = append(cmds, m.setStatus("Clipboard unavailable: "+err.Error()))
		} else {
			cmds = append(cmds, m.setStatus("Row copied to clipboard."))
		}
		return m, tea.Batch(cmds...)

	case exportCSVMsg:
		dir, _ := os.UserHomeDir()
		if r := m.results.Result(); r != nil {
			path, err := r.ExportCSV(dir)
			if err != nil {
				cmds = append(cmds, m.setStatus("Export error: "+err.Error()))
			} else {
				cmds = append(cmds, m.setStatus("Exported to "+path))
			}
		}
		return m, tea.Batch(cmds...)

	case exportJSONMsg:
		dir, _ := os.UserHomeDir()
		if r := m.results.Result(); r != nil {
			path, err := r.ExportJSON(dir)
			if err != nil {
				cmds = append(cmds, m.setStatus("Export error: "+err.Error()))
			} else {
				cmds = append(cmds, m.setStatus("Exported to "+path))
			}
		}
		return m, tea.Batch(cmds...)
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

func schemaKey(connID, dbName string) string {
	if connID == "" {
		return ""
	}
	return connID + ":" + dbName
}

func (m *Model) applyEditorSchema(connID, dbName string) {
	key := schemaKey(connID, dbName)
	m.editor.SetSchema(m.schemaTables[key], m.schemaCols[key])
}

func tableColsKey(connID, dbName, table string) string {
	return connID + ":" + dbName + ":" + table
}

// clearTableColumnCache removes per-table column data for one database (before schema refresh).
func (m *Model) clearTableColumnCache(connID, dbName string) {
	if m.tableCols == nil {
		return
	}
	prefix := schemaKey(connID, dbName) + ":"
	for k := range m.tableCols {
		if strings.HasPrefix(k, prefix) {
			delete(m.tableCols, k)
		}
	}
}

// ingestBulkColumns fills autocomplete + explorer column cache from one information_schema (or equivalent) query.
func (m *Model) ingestBulkColumns(connID, dbName string, rows []db.TableColumn) {
	if len(rows) == 0 {
		return
	}
	prefix := schemaKey(connID, dbName) + ":"
	byTable := make(map[string][]db.ColumnInfo)
	for _, r := range rows {
		if r.Table == "" || r.Name == "" {
			continue
		}
		byTable[r.Table] = append(byTable[r.Table], db.ColumnInfo{Name: r.Name, DataType: r.DataType})
	}
	for tbl, cols := range byTable {
		m.tableCols[prefix+tbl] = cols
	}
	dbKey := schemaKey(connID, dbName)
	seen := make(map[string]struct{}, len(rows)*2)
	tokens := make([]string, 0, len(rows)*2)
	for _, r := range rows {
		if r.Table == "" || r.Name == "" {
			continue
		}
		if _, ok := seen[r.Name]; !ok {
			seen[r.Name] = struct{}{}
			tokens = append(tokens, r.Name)
		}
		q := r.Table + "." + r.Name
		if _, ok := seen[q]; !ok {
			seen[q] = struct{}{}
			tokens = append(tokens, q)
		}
	}
	sort.Strings(tokens)
	m.schemaCols[dbKey] = tokens
	if connID == m.activeConnID && dbName == m.activeDB {
		m.applyEditorSchema(connID, dbName)
	}
}

func (m *Model) persistEditorDraft() {
	if m.queryContents == nil {
		return
	}
	key := m.editor.EditorConnKey()
	if key == "" {
		key = "_"
	}
	_ = m.queryContents.Put(key, m.editor.TabText())
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
			{Key: "x", Description: "Execute query (enter)", Action: func() tea.Msg { return execQueryFromPaletteMsg{} }},
			{Key: "c", Description: "Clear editor", Action: func() tea.Msg { return clearEditorMsg{} }},
			{Key: "t", Description: "Toggle explorer", Action: func() tea.Msg { return toggleExplorerMsg{} }},
			{Key: "f", Description: "Fullscreen", Action: func() tea.Msg { return toggleFullscreenMsg{} }},
		}
	case PanelResults:
		title = "Results Commands"
		commands = []cmdpalette.Command{
			{Key: "y", Description: "Copy cell", Action: func() tea.Msg { return copyCellMsg{} }},
			{Key: "Y", Description: "Copy row", Action: func() tea.Msg { return copyRowMsg{} }},
			{Key: "e", Description: "Export CSV", Action: func() tea.Msg { return exportCSVMsg{} }},
			{Key: "j", Description: "Export JSON", Action: func() tea.Msg { return exportJSONMsg{} }},
			{Key: "t", Description: "Toggle explorer", Action: func() tea.Msg { return toggleExplorerMsg{} }},
			{Key: "f", Description: "Fullscreen", Action: func() tea.Msg { return toggleFullscreenMsg{} }},
		}
	}

	m.palette.Show(title, commands)
}

// activateExplorerDatabase sets the active conn/db, switches the editor tab, and loads history.
// Call before any explorer action that should show that database's query pane (expand/collapse/select table).
func (m *Model) activateExplorerDatabase(connID, dbName string) {
	m.persistEditorDraft()
	m.activeConnID = connID
	m.activeDB = dbName
	m.editor.SwitchConnection(m.connKey())
	m.applyEditorSchema(connID, dbName)
	if m.history != nil {
		entries := m.history.ForKey(m.connKey())
		queries := make([]string, len(entries))
		for i, e := range entries {
			queries[i] = e.Query
		}
		m.editor.SetHistory(queries)
	}
}

func (m *Model) handleExplorerSelect(node *explorer.Node) []tea.Cmd {
	if node == nil {
		return nil
	}
	var cmds []tea.Cmd

	switch node.Kind {
	case explorer.NodeConnection:
		// Only fetch databases when expanding; collapsing should not refetch
		if node.Expanded {
			cmds = append(cmds, m.setStatus("Loading databases..."))
			cmds = append(cmds, m.connectCmd(node.ConnID))
		}

	case explorer.NodeDatabase:
		// Always sync editor + query draft for this database (expand or collapse).
		// Only fetch schema when expanding so collapse does not refetch/re-open.
		m.activateExplorerDatabase(node.ConnID, node.DBName)
		if m.history != nil {
			if n := len(m.history.ForKey(m.connKey())); n > 0 {
				cmds = append(cmds, m.setStatus(fmt.Sprintf("Loaded %d history entries", n)))
			}
		}
		if node.Expanded {
			cmds = append(cmds, m.setStatus("Loading schema..."))
			cmds = append(cmds, m.fetchSchemaCmd(node.ConnID, node.DBName))
		}

	case explorer.NodeTable, explorer.NodeView:
		m.activateExplorerDatabase(node.ConnID, node.DBName)
		if node.Detail == "select" {
			driver := ""
			if conn := m.findConn(node.ConnID); conn != nil {
				driver = conn.Driver
			}
			query := quickSelectQuery(driver, node.DBName, node.Label)
			m.editor.AppendAtEnd(query)
			m.persistEditorDraft()
			// Keep focus in explorer; user can press r to view results.
			m.results.SetLoading(true)
			cmds = append(cmds, m.execQueryCmd(query))
		} else if node.Expanded {
			ck := tableColsKey(node.ConnID, node.DBName, node.Label)
			if cached := m.tableCols[ck]; len(cached) > 0 {
				colNodes := make([]*explorer.Node, 0, len(cached))
				for _, c := range cached {
					colNodes = append(colNodes, explorer.NewColumnNode(c.Name, c.DataType, node.ConnID, node.DBName))
				}
				m.explorer.SetChildren(node.ConnID+":"+node.DBName, node.Label, colNodes)
			} else {
				cmds = append(cmds, m.setStatus("Loading columns..."))
				cmds = append(cmds, m.fetchColumnsCmd(node.ConnID, node.DBName, node.Label))
			}
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
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
		var allCols []db.TableColumn
		if ac, err := driver.AllTableColumns(ctx, dbName); err == nil {
			allCols = ac
		}
		return dbSchemaMsg{connID: connID, dbName: dbName, tables: tables, views: views, allColumns: allCols}
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

func (m *Model) buildDeleteDraftCmd(msg results.DeleteDraftRequestMsg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		conn := m.findConn(m.activeConnID)
		if conn == nil {
			return results.DeleteDraftMsg{Err: "No active connection."}
		}
		drv, err := db.New(*conn)
		if err != nil {
			return results.DeleteDraftMsg{Err: err.Error()}
		}
		if err := drv.Connect(ctx, *conn); err != nil {
			return results.DeleteDraftMsg{Err: err.Error()}
		}
		defer drv.Close()

		schema, tbl := sqlutil.ParseTableRef(msg.TableExpr, msg.Driver)
		var whereCols []string
		pkCols, pkErr := drv.PrimaryKeyColumns(ctx, msg.Database, schema, tbl)
		if pkErr == nil && len(pkCols) > 0 {
			if matched := sqlutil.MatchResultColumnsForPK(msg.Columns, pkCols); matched != nil {
				whereCols = matched
			}
		}
		sqlText, err := sqlutil.DeleteForRows(msg.Driver, msg.TableExpr, msg.Columns, msg.Rows, whereCols)
		if err != nil {
			return results.DeleteDraftMsg{Err: err.Error()}
		}
		return results.DeleteDraftMsg{SQL: sqlText}
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
				result:    &db.QueryResult{Error: "No active connection. Select a database in the explorer first."},
				elapsed:   0,
				sourceSQL: query,
			}
		}
		driver, err := db.New(*conn)
		if err != nil {
			return dbQueryResultMsg{result: &db.QueryResult{Error: err.Error()}, sourceSQL: query}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := driver.Connect(ctx, *conn); err != nil {
			return dbQueryResultMsg{result: &db.QueryResult{Error: err.Error()}, sourceSQL: query}
		}
		defer driver.Close()

		if stmts, ok := sqlutil.SplitExecBatchDeleteUpdate(query); ok && len(stmts) > 1 {
			return execDeleteUpdateBatch(ctx, driver, dbName, stmts, start, query)
		}

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
		return dbQueryResultMsg{result: result, elapsed: time.Since(start), sourceSQL: query}
	}
}

// execDeleteUpdateBatch runs several DELETE/UPDATE statements sequentially (drivers often
// reject multiple statements in a single Exec call).
func execDeleteUpdateBatch(ctx context.Context, driver db.Driver, dbName string, stmts []string, start time.Time, sourceSQL string) dbQueryResultMsg {
	var rows [][]string
	for i, stmt := range stmts {
		result, err := driver.Exec(ctx, dbName, stmt)
		if err != nil {
			return dbQueryResultMsg{
				result:    &db.QueryResult{Error: fmt.Sprintf("statement %d: %v", i+1, err)},
				elapsed:   time.Since(start),
				sourceSQL: sourceSQL,
			}
		}
		if result != nil && result.Error != "" {
			return dbQueryResultMsg{
				result:    &db.QueryResult{Error: fmt.Sprintf("statement %d: %s", i+1, result.Error)},
				elapsed:   time.Since(start),
				sourceSQL: sourceSQL,
			}
		}
		ra := "0"
		if result != nil && len(result.Rows) > 0 && len(result.Rows[0]) > 0 {
			ra = result.Rows[0][0]
		}
		rows = append(rows, []string{fmt.Sprintf("%d", i+1), ra})
	}
	return dbQueryResultMsg{
		result: &db.QueryResult{
			Columns: []string{"#", "rows_affected"},
			Rows:    rows,
		},
		elapsed:   time.Since(start),
		sourceSQL: sourceSQL,
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

func (m *Model) setStatus(msg string) tea.Cmd {
	m.statusMsg = msg
	if msg == "" {
		m.statusExpiry = time.Time{}
		return nil
	}
	seconds := 5
	if m.cfg != nil && m.cfg.StatusMessageSeconds > 0 {
		seconds = m.cfg.StatusMessageSeconds
	}
	m.statusExpiry = time.Now().Add(time.Duration(seconds) * time.Second)
	exp := m.statusExpiry
	return tea.Tick(time.Duration(seconds)*time.Second, func(time.Time) tea.Msg {
		return clearStatusMsg{expiresAt: exp}
	})
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
		return m, m.setStatus("Error saving config: " + err.Error())
	}
	m.showForm = false
	m.connForm = nil
	m.explorer.SetConfig(m.cfg)
	return m, m.setStatus("Connection saved.")
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
		boxed := m.theme.BorderFocused.Width(m.width - 2).Height(m.height - 3).Render(content)
		border := m.embedPanelTopTitle(boxed, m.panelTitleFor(m.fullscreenPanel), true)
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

// rightColumnWidth returns the bordered width for editor/results (matches layoutPanels right split).
func (m Model) rightColumnWidth() int {
	explorerW := m.width * m.cfg.Layout.ExplorerWidthPct / 100
	if m.explorerHidden {
		explorerW = 0
	}
	rightW := m.width - explorerW
	w := rightW - 2
	if w < 0 {
		w = 0
	}
	return w
}

func (m Model) panelTitleFor(p Panel) string {
	switch p {
	case PanelExplorer:
		return "[e] Explorer"
	case PanelEditor:
		sub := "—"
		if m.activeConnID != "" {
			label := m.activeConnID
			if c := m.findConn(m.activeConnID); c != nil && c.Name != "" {
				label = c.Name
			}
			if m.activeDB != "" {
				sub = label + " / " + m.activeDB
			} else {
				sub = label + " —"
			}
		}
		return "[q] Query Editor · " + sub
	case PanelResults:
		return "[r] Results"
	default:
		return ""
	}
}

func (m Model) styleForPanelTopLine(focused bool) lipgloss.Style {
	st := m.theme.BorderFocused
	if !focused {
		st = m.theme.BorderUnfocused
	}
	return lipgloss.NewStyle().Foreground(st.GetBorderTopForeground())
}

// embedPanelTopTitle replaces the first border line of a lipgloss box with a titled top edge.
func (m Model) embedPanelTopTitle(boxed, title string, focused bool) string {
	lines := strings.Split(boxed, "\n")
	if len(lines) == 0 || title == "" {
		return boxed
	}
	tw := ansi.StringWidth(lines[0])
	lines[0] = m.renderPanelTopBorderLine(tw, title, focused)
	return strings.Join(lines, "\n")
}

func (m Model) renderPanelTopBorderLine(outerWidth int, title string, focused bool) string {
	if outerWidth < 3 {
		return strings.Repeat("─", max(0, outerWidth))
	}
	tl := "╭"
	tr := "╮"
	sep := "─"
	mid := outerWidth - ansi.StringWidth(tl) - ansi.StringWidth(tr)
	if mid < 1 {
		return m.styleForPanelTopLine(focused).Render(tl + strings.Repeat(sep, max(0, mid)) + tr)
	}
	leftPart := sep + " " + title + " "
	lpw := ansi.StringWidth(leftPart)
	if lpw > mid {
		inner := mid - ansi.StringWidth(sep+"  ")
		if inner < 1 {
			leftPart = strings.Repeat(sep, mid)
		} else {
			tit := title
			if ansi.StringWidth(tit) > inner {
				tit = ansi.Truncate(tit, inner, "…")
			}
			leftPart = sep + " " + tit + " "
			lpw = ansi.StringWidth(leftPart)
		}
	}
	fill := mid - lpw
	if fill < 0 {
		fill = 0
	}
	line := tl + leftPart + strings.Repeat(sep, fill) + tr
	for ansi.StringWidth(line) < outerWidth {
		rs := []rune(line)
		if len(rs) < 2 {
			break
		}
		body := string(rs[:len(rs)-1])
		corner := string(rs[len(rs)-1:])
		line = body + sep + corner
	}
	return m.styleForPanelTopLine(focused).Render(line)
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
	title := m.panelTitleFor(p)
	switch p {
	case PanelExplorer:
		w := m.width*m.cfg.Layout.ExplorerWidthPct/100 - 2
		h := totalH - 2
		if w < 0 {
			w = 0
		}
		boxed := borderStyle.Width(w).Height(h).Render(content)
		return m.embedPanelTopTitle(boxed, title, focused)
	case PanelEditor:
		w := m.rightColumnWidth()
		h := totalH*m.cfg.Layout.EditorHeightPct/100 - 2
		if h < 0 {
			h = 0
		}
		boxed := borderStyle.Width(w).Height(h).Render(content)
		return m.embedPanelTopTitle(boxed, title, focused)
	case PanelResults:
		w := m.rightColumnWidth()
		h := totalH*(100-m.cfg.Layout.EditorHeightPct)/100 - 2
		if h < 0 {
			h = 0
		}
		boxed := borderStyle.Width(w).Height(h).Render(content)
		return m.embedPanelTopTitle(boxed, title, focused)
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
    enter/l     Expand/collapse node (incl. table columns)
    h           Collapse current branch
    s           Append SELECT * … LIMIT/TOP 100, run it (keeps existing editor text)

  EDITOR (Normal mode)
    i/a/o       Enter insert mode
    enter       Execute query under cursor (batch: only DELETE/UPDATE split by ; → run in order)
    u           Undo edit (whole insert session until esc; normal edits undo separately)
    ctrl+r      Redo edit
    ctrl+p/n    Browse query history (replace buffer)
    backspace   History popup (d = delete confirm panel, y confirm)
    dd          Delete line
    gg/G        Go to top/bottom

  EDITOR (Insert mode)
    esc         Return to normal mode
    tab         Trigger autocomplete
    ctrl+enter  Execute query
    ctrl+r      Execute query

  RESULTS
    j/k         Navigate rows
    h/l         Navigate columns
    g/G         First/last row
    s           Toggle mark on current row (for delete draft)
    S           Mark current row + band-select while moving j/k/g/G (esc clears marks)
    d           Append DELETE draft(s) for marked rows (or cursor row); WHERE uses PK cols when possible
    v           View full selected cell popup (y copy, esc close)

  COMMAND PALETTE (space)
    Explorer:   a=add  e=edit  d=delete  R=refresh  t=toggle explorer  f=fullscreen
    Editor:     x=execute  c=clear  t=toggle explorer  f=fullscreen
    Results:    y=copy cell  Y=copy row  e=export CSV  j=export JSON  t=toggle explorer  f=fullscreen

  Press ? or esc to close
`
	return m.theme.BorderFocused.Width(m.width - 6).Render(
		lipgloss.NewStyle().Padding(1, 2).Render(strings.TrimLeft(help, "\n")),
	)
}

// spliceOverlayLine composites overlay onto base at startCol without breaking ANSI sequences.
func spliceOverlayLine(baseLine, overlay string, startCol, totalWidth int) string {
	ow := lipgloss.Width(overlay)
	if ow == 0 {
		return baseLine
	}
	if startCol < 0 {
		startCol = 0
	}
	maxOW := totalWidth - startCol
	if maxOW < 1 {
		maxOW = 1
	}
	if ow > maxOW {
		overlay = ansi.Truncate(overlay, maxOW, "…")
		ow = lipgloss.Width(overlay)
	}

	baseW := ansi.StringWidth(baseLine)
	left := ansi.Cut(baseLine, 0, startCol)
	if lw := ansi.StringWidth(left); lw < startCol {
		left += strings.Repeat(" ", startCol-lw)
	}

	right := ansi.Cut(baseLine, startCol+ow, baseW)
	merged := left + overlay + right
	mw := ansi.StringWidth(merged)
	switch {
	case mw < totalWidth:
		merged += strings.Repeat(" ", totalWidth-mw)
	case mw > totalWidth:
		merged = ansi.Truncate(merged, totalWidth, "")
	}
	return merged
}

func overlayCentered(base, overlay string, width, height int) string {
	_ = height
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")

	overlayH := len(overlayLines)
	overlayW := 0
	for _, l := range overlayLines {
		if lw := lipgloss.Width(l); lw > overlayW {
			overlayW = lw
		}
	}

	startRow := (len(baseLines) - overlayH) / 2
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
		baseLines[row] = spliceOverlayLine(baseLines[row], ol, startCol, width)
	}
	return strings.Join(baseLines, "\n")
}

func overlayBottomRight(base, overlay string, width, height int) string {
	_ = height
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")

	overlayW := 0
	for _, l := range overlayLines {
		if lw := lipgloss.Width(l); lw > overlayW {
			overlayW = lw
		}
	}

	// Sit above the status bar (last line of the frame)
	startRow := len(baseLines) - len(overlayLines) - 1
	if startRow < 0 {
		startRow = 0
	}
	startCol := width - overlayW - 1
	if startCol < 0 {
		startCol = 0
	}

	for i, ol := range overlayLines {
		row := startRow + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLines[row] = spliceOverlayLine(baseLines[row], ol, startCol, width)
	}

	return strings.Join(baseLines, "\n")
}
