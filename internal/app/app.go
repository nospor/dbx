package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	internalAi "github.com/robertn/dbx/internal/ai"
	"github.com/robertn/dbx/internal/config"
	"github.com/robertn/dbx/internal/db"
	"github.com/robertn/dbx/internal/history"
	"github.com/robertn/dbx/internal/opentabs"
	"github.com/robertn/dbx/internal/querycontents"
	"github.com/robertn/dbx/internal/sqlutil"
	"github.com/robertn/dbx/internal/ui/ai"
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
	openTabsStore *opentabs.Store

	width   int
	height  int
	workDir string

	focus Panel

	explorerHidden  bool
	aiHidden        bool
	fullscreenOn    bool
	fullscreenPanel Panel

	// Active connection state
	activeConnID  string
	activeDB      string
	drivers       map[string]db.Driver           // connID -> driver
	schemaTables  map[string][]string            // connID:db -> tables/views
	schemaViewSet map[string]map[string]struct{} // connID:db -> view names (for TableDDL isView)
	schemaCols    map[string][]string            // connID:db -> column tokens
	tableCols     map[string][]db.ColumnInfo     // connID:db:table -> columns (cache + explorer)

	// Per editor-tab results for this session only (key = connID:dbName).
	tabResultCache   map[string]*results.QueryResult
	tabResultLoading map[string]bool

	explorer   explorer.Model
	editor     editor.Model
	results    results.Model
	aiPane     ai.Model
	aiStore    *internalAi.Store
	palette    cmdpalette.Model
	connForm   *explorer.ConnForm
	showForm   bool
	showHelp   bool
	helpScroll int

	spinner   spinner.Model
	isLoading bool

	statusMsg    string
	statusExpiry time.Time

	// Explorer table/view DDL popup (v)
	ddlPopupOpen   bool
	ddlPopupTitle  string
	ddlPopupText   string
	ddlPopupScroll int

	// Editor tabs + restore
	tabCloseConfirm             bool
	pendingExplorerSelectConnID string
	pendingExplorerSelectDB     string
}

func newEditorWithDrafts(t theme.Theme, drafts map[string]string, cfg *config.Config, ot *opentabs.Store) editor.Model {
	ed := editor.New(t)
	ed.SeedQueryTabs(drafts)
	keys := ot.Keys()
	valid := make([]string, 0, len(keys))
	for _, k := range keys {
		connID, _ := splitConnKey(k)
		found := false
		for _, c := range cfg.Connections {
			if c.ID == connID {
				found = true
				break
			}
		}
		if found {
			valid = append(valid, k)
		}
	}
	wantActive := ot.ActiveKey()
	validActive := ""
	if wantActive != "" {
		for _, k := range valid {
			if k == wantActive {
				validActive = wantActive
				break
			}
		}
	}
	ed.RestoreOpenTabs(valid, validActive, func(s string) string { return tabLabelForConfig(cfg, s) })
	return ed
}

// New creates the root application model. workDir should be the process working directory at startup (e.g. os.Getwd()) for folder-scoped query tabs when cfg.FolderBased is true.
func New(cfg *config.Config, workDir string) Model {
	t := theme.Get(cfg.Theme)

	hist := history.NewOrEmpty()
	qc := querycontents.NewOrEmpty(cfg.FolderBased, workDir)
	ot := opentabs.NewOrEmpty(cfg.FolderBased, workDir)

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	aiStore := internalAi.LoadStore(cfg)
	m := Model{
		cfg:              cfg,
		theme:            t,
		keymap:           DefaultKeyMap(),
		history:          hist,
		queryContents:    qc,
		openTabsStore:    ot,
		focus:            PanelEditor,
		fullscreenPanel:  PanelEditor,
		explorerHidden:   *cfg.Layout.ExplorerHidden,
		aiHidden:         *cfg.Layout.AIPaneHidden,
		aiStore:          aiStore,
		drivers:          make(map[string]db.Driver),
		schemaTables:     make(map[string][]string),
		schemaViewSet:    make(map[string]map[string]struct{}),
		schemaCols:       make(map[string][]string),
		tableCols:        make(map[string][]db.ColumnInfo),
		tabResultCache:   make(map[string]*results.QueryResult),
		tabResultLoading: make(map[string]bool),
		explorer:         explorer.New(cfg, t),
		editor:           newEditorWithDrafts(t, qc.All(), cfg, ot),
		results:          results.New(t),
		aiPane:           ai.New(t, aiStore),
		palette:          cmdpalette.New(t),
		spinner:          sp,
		workDir:          workDir,
	}
	if kt := m.editor.OpenTabKeys(); len(kt) > 0 {
		connID, dbName := splitConnKey(m.editor.EditorConnKey())
		m.activeConnID = connID
		m.activeDB = dbName
		if connID != "" && dbName != "" {
			m.pendingExplorerSelectConnID = connID
			m.pendingExplorerSelectDB = dbName
		}
	}
	(&m).setFocus(PanelEditor)
	return m
}

// clearStatusMsg is a delayed message to clear the status bar.
type clearStatusMsg struct {
	expiresAt time.Time
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}
	if m.pendingExplorerSelectConnID != "" {
		cmds = append(cmds, m.connectCmd(m.pendingExplorerSelectConnID))
	}
	return tea.Batch(cmds...)
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

	case tableDDLMsg:
		if msg.err != nil {
			return m, m.setStatus("DDL: " + msg.err.Error())
		}
		m.ddlPopupOpen = true
		m.ddlPopupTitle = msg.title
		m.ddlPopupText = msg.ddl
		m.ddlPopupScroll = 0
		return m, nil

	case exportAllDDLResultMsg:
		if msg.err != nil {
			return m, m.setStatus("Export error: " + msg.err.Error())
		}
		// Use a slightly different prefix to make it stand out
		return m, m.setStatus("SUCCESS: All DDLs exported to " + msg.path)

	case results.DeleteDraftMsg:
		if msg.Err != "" {
			return m, m.setStatus(msg.Err)
		}
		m.appendDeleteUpdateDraft(msg.SQL)
		m.persistEditorDraft()
		m.setFocus(PanelEditor)
		return m, m.setStatus("DELETE draft appended — review before running.")

	case results.DeleteDraftRequestMsg:
		return m, m.buildDeleteDraftCmd(msg)

	case results.InsertDraftMsg:
		if msg.Err != "" {
			return m, m.setStatus(msg.Err)
		}
		m.appendDeleteUpdateDraft(msg.SQL)
		m.persistEditorDraft()
		m.setFocus(PanelEditor)
		return m, m.setStatus("INSERT draft appended — review before running.")

	case results.UpdateDraftMsg:
		if msg.Err != "" {
			return m, m.setStatus(msg.Err)
		}
		m.appendDeleteUpdateDraft(msg.SQL)
		m.persistEditorDraft()
		return m, m.setStatus("UPDATE draft appended — review before running.")

	case results.UpdateDraftRequestMsg:
		return m, m.buildUpdateDraftCmd(msg)

	case explorer.ConnFormSubmitMsg:
		return m.handleConnFormSubmit(msg)

	case explorer.ConnFormCancelMsg:
		m.showForm = false
		m.connForm = nil
		return m, nil

	case explorer.ConnTestRequestMsg:
		return m, m.testConnectionCmd(msg.Conn)

	case explorer.ConnTestResultMsg:
		if m.showForm && m.connForm != nil {
			updated, cmd := m.connForm.Update(msg)
			m.connForm = &updated
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		// Route to connection form if active
		if m.showForm && m.connForm != nil {
			updated, cmd := m.connForm.Update(msg)
			m.connForm = &updated
			return m, cmd
		}

		if m.tabCloseConfirm {
			switch msg.String() {
			case "y", "enter":
				m.tabCloseConfirm = false
				m.persistEditorDraft()
				sw := m.editor.CloseActiveTab()
				if sw != nil {
					sub := m.syncActiveFromEditorTab(sw.ConnKey)
					sub = append(sub, m.setStatus("Tab closed."))
					return m, tea.Batch(sub...)
				}
				m.persistOpenTabs()
				return m, m.setStatus("Tab closed.")
			case "n", "esc", "q":
				m.tabCloseConfirm = false
				return m, m.setStatus("")
			}
			return m, nil
		}

		if m.ddlPopupOpen {
			return m.handleDDLPopupKey(msg)
		}

		if m.showHelp {
			return m.handleHelpPopupKey(msg)
		}

		if m.palette.IsVisible() {
			var cmd tea.Cmd
			m.palette, cmd = m.palette.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		// Global keys — suppressed in insert mode, history popup, explorer filtering, and AI input mode.
		editorInsert := m.editor.IsInsertMode()
		historyPopup := m.editor.HistoryPopupVisible()
		explorerFiltering := m.explorer.IsFiltering()
		// While AI answers, stay navigable: insert mode suppresses e/q/r/space unless loading (prompt in flight).
		aiInput := m.aiPane.IsInputMode() && !m.aiPane.IsLoading()
		suppressGlobals := editorInsert || historyPopup || explorerFiltering || aiInput

		switch msg.String() {
		case "ctrl+c":
			m.persistEditorDraft()
			m.persistOpenTabs()
			m.closeAllDrivers()
			return m, tea.Quit

		case "e":
			if !suppressGlobals {
				m.setFocus(PanelExplorer)
				return m, nil
			}

		case "q":
			if !suppressGlobals && m.focus != PanelEditor {
				m.setFocus(PanelEditor)
				return m, nil
			}

		case "a":
			if !suppressGlobals && m.focus != PanelAI {
				if m.aiHidden {
					m.aiHidden = false
					m.aiPane.SetConnKey(m.connKey())
					m.layoutPanels()
					m.persistPaneVisibility()
				}
				m.setFocus(PanelAI)
				return m, nil
			}

		case "r":
			if !suppressGlobals {
				m.setFocus(PanelResults)
				return m, nil
			}

		case " ":
			if !suppressGlobals {
				m.openPalette()
				return m, nil
			}

		case "?":
			if !suppressGlobals {
				m.showHelp = !m.showHelp
				if m.showHelp {
					m.helpScroll = 0
				}
				return m, nil
			}

		}

	case cmdpalette.ExecuteCommandMsg:
		if msg.Action != nil {
			cmds = append(cmds, func() tea.Msg { return msg.Action() })
		}
		return m, tea.Batch(cmds...)

	case editor.TabSwitchedMsg:
		sub := m.syncActiveFromEditorTab(msg.ConnKey)
		return m, tea.Batch(sub...)

	case closeTabPromptMsg:
		if len(m.editor.OpenTabKeys()) == 0 {
			return m, m.setStatus("No tab to close.")
		}
		m.tabCloseConfirm = true
		return m, nil

	case editor.ExecuteQueryMsg:
		if msg.Query == "" {
			return m, nil
		}
		m.beginQueryForActiveTab()
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
		if msg.result == nil {
			msg.result = &db.QueryResult{Error: "internal error: nil query result"}
		}
		k := m.tabResultCacheKey(msg.connID, msg.dbName)
		if k != "" {
			m.tabResultLoading[k] = false
		}
		drv := ""
		if c := m.findConn(msg.connID); c != nil {
			drv = c.Driver
		}
		qr := &results.QueryResult{
			Columns:   msg.result.Columns,
			Rows:      msg.result.Rows,
			Error:     msg.result.Error,
			Elapsed:   msg.elapsed,
			SourceSQL: msg.sourceSQL,
			Driver:    drv,
			Database:  msg.dbName,
		}
		if k != "" {
			m.tabResultCache[k] = results.CloneQueryResult(qr)
		}
		belongsToActive := m.tabResultCacheKey(msg.connID, msg.dbName) == m.tabResultCacheKey(m.activeConnID, m.activeDB)
		if belongsToActive {
			m.results.SetResult(qr)
		}
		m.syncIsLoadingWithActiveTab()
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
		viewSet := make(map[string]struct{}, len(msg.views))
		for _, v := range msg.views {
			viewSet[v] = struct{}{}
		}
		m.schemaViewSet[key] = viewSet
		m.clearTableColumnCache(msg.connID, msg.dbName)
		m.schemaCols[key] = nil
		if len(msg.allColumns) > 0 {
			m.ingestBulkColumns(msg.connID, msg.dbName, msg.allColumns)
		} else if msg.connID == m.activeConnID && msg.dbName == m.activeDB {
			m.applyEditorSchema(msg.connID, msg.dbName)
		}
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
		if m.pendingExplorerSelectConnID == msg.connID && m.pendingExplorerSelectDB != "" {
			db := m.pendingExplorerSelectDB
			if m.explorer.SelectDatabaseNode(msg.connID, db) {
				m.pendingExplorerSelectConnID = ""
				m.pendingExplorerSelectDB = ""
				n := explorer.NewDatabaseNode(db, msg.connID)
				n.Expanded = true
				cmds = append(cmds, func() tea.Msg {
					return explorerSelectMsg{node: n}
				})
			}
		} else if len(msg.databases) == 1 {
			// Auto-select when connection has exactly one database (loads history, fetches schema)
			cmds = append(cmds, func() tea.Msg {
				return explorerSelectMsg{node: explorer.NewDatabaseNode(msg.databases[0], msg.connID)}
			})
		}
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
		m.persistPaneVisibility()
		return m, nil

	case toggleAIPaneMsg:
		m.aiHidden = !m.aiHidden
		if m.aiHidden && m.focus == PanelAI {
			m.setFocus(PanelEditor)
		}
		if !m.aiHidden {
			// Ensure AI pane is synced with current connection/db when revealed
			m.aiPane.SetConnKey(m.connKey())
		}
		m.layoutPanels()
		m.persistPaneVisibility()
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

	case exportAllDDLFromPaletteMsg:
		if node := m.explorer.SelectedNode(); node != nil && node.DBName != "" {
			cmds = append(cmds, m.setStatusPersistent("Exporting all DDLs..."))
			cmds = append(cmds, m.exportAllDDLCmd(node.ConnID, node.DBName))
		}
		return m, tea.Batch(cmds...)

	case fetchTableDDLFromPaletteMsg:
		if node := m.explorer.SelectedNode(); node != nil && (node.Kind == explorer.NodeTable || node.Kind == explorer.NodeView) {
			m.activateExplorerDatabase(node.ConnID, node.DBName)
			isView := node.Kind == explorer.NodeView
			cmds = append(cmds, m.setStatus("Loading DDL..."))
			cmds = append(cmds, m.fetchTableDDLCmd(node.ConnID, node.DBName, node.Label, isView))
		}
		return m, tea.Batch(cmds...)

	case execQueryFromPaletteMsg:
		query := m.editor.CurrentQuery()
		if query != "" {
			m.beginQueryForActiveTab()
			cmds = append(cmds, m.execQueryCmd(query))
		}
		return m, tea.Batch(cmds...)

	case explainQueryFromPaletteMsg:
		drv := ""
		if c := m.findConn(m.activeConnID); c != nil {
			drv = c.Driver
		}
		if m.editor.WrapCurrentQueryExplain(drv) {
			m.persistEditorDraft()
		}
		return m, nil

	case clearEditorMsg:
		m.editor.ClearUndoable()
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

	case copyAllRowsMsg:
		r := m.results.Result()
		if r == nil || len(r.Columns) == 0 {
			cmds = append(cmds, m.setStatus("No results to copy."))
			return m, tea.Batch(cmds...)
		}
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		var trimmedCols []string
		for _, col := range r.Columns {
			trimmedCols = append(trimmedCols, strings.TrimSpace(col))
		}
		if err := w.Write(trimmedCols); err != nil {
			cmds = append(cmds, m.setStatus("Failed to format headers: "+err.Error()))
			return m, tea.Batch(cmds...)
		}
		for _, row := range r.Rows {
			if err := w.Write(row); err != nil {
				cmds = append(cmds, m.setStatus("Failed to format rows: "+err.Error()))
				return m, tea.Batch(cmds...)
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			cmds = append(cmds, m.setStatus("CSV formatting error: "+err.Error()))
			return m, tea.Batch(cmds...)
		}
		if err := util.Copy(buf.String()); err != nil {
			cmds = append(cmds, m.setStatus("Clipboard unavailable: "+err.Error()))
		} else {
			cmds = append(cmds, m.setStatus("All rows (CSV with headers) copied to clipboard."))
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

	case ai.ExtractSQLMsg:
		m.editor.AppendAtEnd(msg.SQL)
		m.setFocus(PanelEditor)
		return m, m.setStatus("Extracted query from AI to Editor.")

	case ai.AISendPromptMsg:
		if msg.IncludeResults {
			if m.results.Loading() {
				key := schemaKey(m.activeConnID, m.activeDB)
				var cmd tea.Cmd
				m.aiPane, cmd = m.aiPane.Update(ai.AIPromptAbortedMsg{
					ConnKey:     msg.ConnKey,
					RestoreText: msg.OriginalInput,
				}, m.schemaTables[key], m.schemaCols[key])
				cmds = append(cmds, cmd)
				cmds = append(cmds, m.setStatus("Results are still loading; wait for the query to finish."))
				return m, tea.Batch(cmds...)
			}
			r := m.results.Result()
			if r == nil || strings.TrimSpace(r.SourceSQL) == "" || r.Error != "" {
				key := schemaKey(m.activeConnID, m.activeDB)
				var cmd tea.Cmd
				m.aiPane, cmd = m.aiPane.Update(ai.AIPromptAbortedMsg{
					ConnKey:     msg.ConnKey,
					RestoreText: msg.OriginalInput,
				}, m.schemaTables[key], m.schemaCols[key])
				cmds = append(cmds, cmd)
				status := "No query results to attach; run a successful query in the results pane first."
				if r != nil && r.Error != "" {
					status = "Cannot attach results: the last query failed. Fix the query and run it again."
				}
				cmds = append(cmds, m.setStatus(status))
				return m, tea.Batch(cmds...)
			}
		}
		transcript := msg.Transcript
		if transcript == "" {
			transcript = msg.Prompt
		}
		var sysPrefix string
		if m.aiStore != nil {
			chat := m.aiStore.GetSession(msg.ConnKey)
			// Only before AppendUserMessageAt: after append, len would always be ≥ 1.
			if len(chat.Messages) == 0 {
				sysPrefix = aiOutboundSystemPrefix
			}
		}
		m.aiPane.AppendUserMessageAt(msg.ConnKey, transcript) // chat shows what the user sent (/results line or plain prompt)
		cmds = append(cmds, m.prepareAISendCmd(msg, sysPrefix))
		return m, tea.Batch(cmds...)

	case aiPreparedPromptMsg:
		cmds = append(cmds, ai.AskCmd(m.aiStore, msg.connKey, msg.fullPrompt))
		return m, tea.Batch(cmds...)

	case ai.AIResponseMsg:
		// ALWAYS route to AI pane globally, since AI background call can return anytime
		var cmd tea.Cmd
		key := schemaKey(m.activeConnID, m.activeDB)
		m.aiPane, cmd = m.aiPane.Update(msg, m.schemaTables[key], m.schemaCols[key])
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)

	case ai.AISessionResetMsg:
		var cmd tea.Cmd
		key := schemaKey(m.activeConnID, m.activeDB)
		m.aiPane, cmd = m.aiPane.Update(msg, m.schemaTables[key], m.schemaCols[key])
		cmds = append(cmds, cmd)
		if msg.Err != nil {
			cmds = append(cmds, m.setStatus("AI /clear failed: "+msg.Err.Error()))
		} else {
			cmds = append(cmds, m.setStatus("AI chat cleared; new session started."))
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
	case PanelAI:
		var cmd tea.Cmd
		key := schemaKey(m.activeConnID, m.activeDB)
		m.aiPane, cmd = m.aiPane.Update(msg, m.schemaTables[key], m.schemaCols[key])
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) setFocus(p Panel) {
	m.focus = p
	m.explorer.SetFocused(p == PanelExplorer)
	m.editor.SetFocused(p == PanelEditor)
	m.results.SetFocused(p == PanelResults)
	m.aiPane.SetFocused(p == PanelAI)
	if m.fullscreenOn {
		m.fullscreenPanel = p
	}
}

func (m *Model) connKey() string {
	if m.activeConnID == "" {
		return ""
	}
	return m.activeConnID + ":" + m.activeDB
}

// tabResultCacheKey matches editor tab keys (connID:dbName).
func (m *Model) tabResultCacheKey(connID, dbName string) string {
	if connID == "" {
		return ""
	}
	return connID + ":" + dbName
}

func (m *Model) syncIsLoadingWithActiveTab() {
	k := m.tabResultCacheKey(m.activeConnID, m.activeDB)
	if k == "" {
		m.isLoading = false
		return
	}
	m.isLoading = m.tabResultLoading[k]
}

func (m *Model) stashResultsForTab(connID, dbName string) {
	k := m.tabResultCacheKey(connID, dbName)
	if k == "" {
		return
	}
	m.tabResultCache[k] = results.CloneQueryResult(m.results.Result())
	m.tabResultLoading[k] = m.results.Loading()
}

func (m *Model) applyResultsForTab(connID, dbName string) {
	k := m.tabResultCacheKey(connID, dbName)
	if k == "" {
		m.results.SetResult(nil)
		m.results.SetLoading(false)
		m.isLoading = false
		return
	}
	loading := m.tabResultLoading[k]
	r := m.tabResultCache[k]
	if r != nil {
		m.results.SetResult(r)
	} else {
		m.results.SetResult(nil)
	}
	if loading {
		m.results.SetLoading(true)
	}
	m.syncIsLoadingWithActiveTab()
}

func (m *Model) beginQueryForActiveTab() {
	k := m.tabResultCacheKey(m.activeConnID, m.activeDB)
	if k == "" {
		m.results.SetLoading(true)
		m.isLoading = true
		return
	}
	m.tabResultLoading[k] = true
	m.results.SetLoading(true)
	m.isLoading = true
}

func (m *Model) pruneTabResultsCache() {
	keys := m.editor.OpenTabKeys()
	open := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		open[k] = struct{}{}
	}
	for k := range m.tabResultCache {
		if _, ok := open[k]; !ok {
			delete(m.tabResultCache, k)
		}
	}
	for k := range m.tabResultLoading {
		if _, ok := open[k]; !ok {
			delete(m.tabResultLoading, k)
		}
	}
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

// aiOutboundSystemPrefix is prepended only to the first outbound prompt after the transcript
// is empty (new chat or /clear), before optional @-mention DDL blocks and the user’s text.
// It is not shown in the transcript — only the raw user line is.
const aiOutboundSystemPrefix = "You are a database assistant. This conversation is about SQL and database data — " +
	"NOT about source code or files. Do not search the filesystem or codebase. " +
	"Answer questions about queries, data, and database structure.\n\n" +
	"When you include SQL, wrap each query in a fenced code block exactly like this:\n```sql\n" +
	"...your SQL here...\n```\n\n"

// collectAIMentionedTables returns unique table/view names from @tokens in the prompt.
// @all expands to every table and view in the schema for connID:dbName.
func (m *Model) collectAIMentionedTables(connID, dbName, userPrompt string) []string {
	sk := schemaKey(connID, dbName)
	words := strings.Fields(userPrompt)
	var mentioned []string
	seen := make(map[string]bool)
	for _, w := range words {
		if !strings.HasPrefix(w, "@") {
			continue
		}
		table := strings.TrimPrefix(w, "@")
		table = strings.TrimRight(table, ".,;:!?)")
		if table == "" {
			continue
		}
		if strings.EqualFold(table, "all") {
			for _, t := range m.schemaTables[sk] {
				if !seen[t] {
					mentioned = append(mentioned, t)
					seen[t] = true
				}
			}
			continue
		}
		if seen[table] {
			continue
		}
		seen[table] = true
		mentioned = append(mentioned, table)
	}
	return mentioned
}

const aiResultsSQLMaxBytes = 32000

func sanitizeAIResultCell(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return s
}

func sanitizeAIResultRow(row []string, wantCols int) []string {
	out := make([]string, wantCols)
	for i := 0; i < wantCols; i++ {
		if i < len(row) {
			out[i] = sanitizeAIResultCell(row[i])
		}
	}
	return out
}

// formatAIResultsContextBlock builds query + TSV rows for the AI, capped at maxBytes (UTF-8 length).
func formatAIResultsContextBlock(r *results.QueryResult, maxBytes int) string {
	if r == nil {
		return ""
	}
	sqlPart := strings.TrimSpace(r.SourceSQL)
	if len(sqlPart) > aiResultsSQLMaxBytes {
		sqlPart = sqlPart[:aiResultsSQLMaxBytes] + "\n-- … query truncated for context size\n"
	}
	var sb strings.Builder
	hdr := "## Results pane context\n\n### Query\n\n```sql\n" + sqlPart + "\n```\n\n### Rows (tab-separated)\n\n"
	if len(hdr) >= maxBytes {
		return "## Results pane context\n\n(Context limit too small for query + rows.)\n\n"
	}
	sb.WriteString(hdr)
	nc := len(r.Columns)
	if nc == 0 && len(r.Rows) > 0 {
		nc = len(r.Rows[0])
	}
	if nc > 0 && len(r.Columns) > 0 {
		line := strings.Join(sanitizeAIResultRow(r.Columns, nc), "\t") + "\n"
		if sb.Len()+len(line) > maxBytes {
			sb.WriteString("(header row omitted — context limit exceeded)\n\n")
			return sb.String()
		}
		sb.WriteString(line)
	}
	included := 0
	for _, row := range r.Rows {
		line := strings.Join(sanitizeAIResultRow(row, nc), "\t") + "\n"
		if sb.Len()+len(line) > maxBytes {
			fmt.Fprintf(&sb, "\n… truncated: showing %d of %d rows (max ~%d KB for this block).\n", included, len(r.Rows), max(1, maxBytes/1024))
			break
		}
		sb.WriteString(line)
		included++
	}
	return sb.String()
}

// prepareAISendCmd fetches driver TableDDL for each @-mentioned object and builds the full prompt:
// systemPrefix + optional "## Database context" (DDL) + optional results pane block + user prompt.
func (m *Model) prepareAISendCmd(msg ai.AISendPromptMsg, systemPrefix string) tea.Cmd {
	connID, dbName := splitConnKey(msg.ConnKey)
	tables := m.collectAIMentionedTables(connID, dbName, msg.Prompt)
	sk := schemaKey(connID, dbName)
	var viewsSnap map[string]struct{}
	if vs := m.schemaViewSet[sk]; len(vs) > 0 {
		viewsSnap = maps.Clone(vs)
	}
	tablesCopy := append([]string(nil), tables...)
	conn := m.findConn(connID)
	userPrompt := msg.Prompt
	connKey := msg.ConnKey
	prefix := systemPrefix

	maxResultsBytes := 256 * 1024
	if m.cfg != nil && m.cfg.AI != nil && m.cfg.AI.MaxResultsContextKB > 0 {
		maxResultsBytes = m.cfg.AI.MaxResultsContextKB * 1024
	}
	var resultsSnap *results.QueryResult
	if msg.IncludeResults {
		resultsSnap = results.CloneQueryResult(m.results.Result())
	}

	return func() tea.Msg {
		var sb strings.Builder
		sb.WriteString(prefix)
		if len(tablesCopy) > 0 {
			sb.WriteString("## Database context\n\n")
			if conn == nil {
				sb.WriteString("(No connection: could not load DDL for @mentions.)\n\n")
			} else {
				drv, err := db.New(*conn)
				if err != nil {
					sb.WriteString("(Could not open database driver: " + err.Error() + ")\n\n")
				} else {
					ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
					defer cancel()
					if err := drv.Connect(ctx, *conn); err != nil {
						sb.WriteString("(Could not connect: " + err.Error() + ")\n\n")
					} else {
						defer drv.Close()
						for _, tbl := range tablesCopy {
							isView := false
							if viewsSnap != nil {
								_, isView = viewsSnap[tbl]
							}
							ddl, err := drv.TableDDL(ctx, dbName, tbl, isView)
							if err != nil {
								alt := !isView
								ddl2, err2 := drv.TableDDL(ctx, dbName, tbl, alt)
								if err2 == nil {
									ddl, err = ddl2, nil
								}
							}
							if err != nil {
								sb.WriteString("### `" + tbl + "`\n/* Error loading DDL: " + err.Error() + " */\n\n")
								continue
							}
							sb.WriteString("### `" + tbl + "`\n```sql\n" + strings.TrimSpace(ddl) + "\n```\n\n")
						}
					}
				}
			}
		}
		if resultsSnap != nil {
			sb.WriteString(formatAIResultsContextBlock(resultsSnap, maxResultsBytes))
		}
		sb.WriteString(userPrompt)
		return aiPreparedPromptMsg{connKey: connKey, fullPrompt: sb.String()}
	}
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
			{Key: "n", Description: "Add connection", Action: func() tea.Msg { return addConnMsg{} }},
			{Key: "e", Description: "Edit connection", Action: func() tea.Msg { return editConnMsg{} }},
			{Key: "d", Description: "Delete connection", Action: func() tea.Msg { return deleteConnMsg{} }},
			{Key: "v", Description: "Show DDL", Action: func() tea.Msg { return fetchTableDDLFromPaletteMsg{} }},
			{Key: "V", Description: "Export all DDLs", Action: func() tea.Msg { return exportAllDDLFromPaletteMsg{} }},
			{Key: "R", Description: "Refresh schema", Action: func() tea.Msg { return refreshSchemaMsg{} }},
			{Key: "t", Description: "Toggle explorer", Action: func() tea.Msg { return toggleExplorerMsg{} }},
			{Key: "a", Description: "Toggle AI Pane", Action: func() tea.Msg { return toggleAIPaneMsg{} }},
			{Key: "f", Description: "Fullscreen", Action: func() tea.Msg { return toggleFullscreenMsg{} }},
		}
	case PanelEditor:
		title = "Editor Commands"
		commands = []cmdpalette.Command{
			{Key: "x", Description: "Execute query (enter)", Action: func() tea.Msg { return execQueryFromPaletteMsg{} }},
			{Key: "e", Description: "Explain current query", Action: func() tea.Msg { return explainQueryFromPaletteMsg{} }},
			{Key: "c", Description: "Clear editor", Action: func() tea.Msg { return clearEditorMsg{} }},
			{Key: "D", Description: "Close tab (confirm)", Action: func() tea.Msg { return closeTabPromptMsg{} }},
			{Key: "t", Description: "Toggle explorer", Action: func() tea.Msg { return toggleExplorerMsg{} }},
			{Key: "a", Description: "Toggle AI Pane", Action: func() tea.Msg { return toggleAIPaneMsg{} }},
			{Key: "f", Description: "Fullscreen", Action: func() tea.Msg { return toggleFullscreenMsg{} }},
		}
	case PanelResults:
		title = "Results Commands"
		commands = []cmdpalette.Command{
			{Key: "y", Description: "Copy cell", Action: func() tea.Msg { return copyCellMsg{} }},
			{Key: "Y", Description: "Copy row", Action: func() tea.Msg { return copyRowMsg{} }},
			{Key: "c", Description: "Copy all rows (headers)", Action: func() tea.Msg { return copyAllRowsMsg{} }},
			{Key: "e", Description: "Export CSV", Action: func() tea.Msg { return exportCSVMsg{} }},
			{Key: "j", Description: "Export JSON", Action: func() tea.Msg { return exportJSONMsg{} }},
			{Key: "t", Description: "Toggle explorer", Action: func() tea.Msg { return toggleExplorerMsg{} }},
			{Key: "a", Description: "Toggle AI Pane", Action: func() tea.Msg { return toggleAIPaneMsg{} }},
			{Key: "f", Description: "Fullscreen", Action: func() tea.Msg { return toggleFullscreenMsg{} }},
		}
	case PanelAI:
		title = "AI Commands"
		commands = []cmdpalette.Command{
			{Key: "t", Description: "Toggle explorer", Action: func() tea.Msg { return toggleExplorerMsg{} }},
			{Key: "a", Description: "Toggle AI Pane", Action: func() tea.Msg { return toggleAIPaneMsg{} }},
			{Key: "f", Description: "Fullscreen", Action: func() tea.Msg { return toggleFullscreenMsg{} }},
		}
	}

	m.palette.Show(title, commands)
}

func (m *Model) tabLabelForConn(connID, dbName string) string {
	label := connID
	if c := m.findConn(connID); c != nil && c.Name != "" {
		label = c.Name
	}
	if dbName != "" {
		return label + " / " + dbName
	}
	return label
}

func (m *Model) persistOpenTabs() {
	if m.openTabsStore == nil {
		return
	}
	_ = m.openTabsStore.Save(m.editor.OpenTabKeys(), m.editor.EditorConnKey())
}

// syncActiveFromEditorTab applies app + explorer state when the editor active tab changes (keyboard).
func (m *Model) syncActiveFromEditorTab(connKey string) []tea.Cmd {
	var cmds []tea.Cmd
	prevConn, prevDB := m.activeConnID, m.activeDB
	if connKey == "" {
		if prevConn != "" {
			m.explorer.CollapseConnection(prevConn)
		}
		m.stashResultsForTab(prevConn, prevDB)
		m.activeConnID = ""
		m.activeDB = ""
		m.applyResultsForTab("", "")
		m.pruneTabResultsCache()
		m.persistOpenTabs()
		m.aiPane.SetConnKey("")
		return cmds
	}
	connID, dbName := splitConnKey(connKey)
	m.stashResultsForTab(prevConn, prevDB)
	if prevConn != "" && (prevConn != connID || prevDB != dbName) {
		if prevConn != connID {
			m.explorer.CollapseConnection(prevConn)
		} else if prevDB != "" {
			m.explorer.CollapseDatabaseSubtree(prevConn, prevDB)
		} else {
			m.explorer.CollapseConnection(prevConn)
		}
	}
	m.activeConnID = connID
	m.activeDB = dbName
	m.applyResultsForTab(connID, dbName)
	m.pruneTabResultsCache()
	if m.explorer.SelectDatabaseNode(connID, dbName) {
		// Aligning the tree does not run the same path as Enter on a database (handleExplorerSelect),
		// so schema may never load (e.g. switching to another restored tab). Fetch if not loaded yet.
		sk := schemaKey(connID, dbName)
		if _, ok := m.schemaTables[sk]; !ok {
			cmds = append(cmds, m.setStatus("Loading schema..."))
			cmds = append(cmds, m.fetchSchemaCmd(connID, dbName))
		}
	} else if connID != "" && dbName != "" {
		m.pendingExplorerSelectConnID = connID
		m.pendingExplorerSelectDB = dbName
		cmds = append(cmds, m.connectCmd(connID))
	}
	m.applyEditorSchema(connID, dbName)
	if m.history != nil {
		entries := m.history.ForKey(connKey)
		queries := make([]string, len(entries))
		for i, e := range entries {
			queries[i] = e.Query
		}
		m.editor.SetHistory(queries)
	}
	m.persistOpenTabs()
	m.aiPane.SetConnKey(connKey)
	return cmds
}

// activateExplorerDatabase sets the active conn/db, switches the editor tab, and loads history.
// Call before any explorer action that should show that database's query pane (expand/collapse/select table).
func (m *Model) activateExplorerDatabase(connID, dbName string) {
	m.persistEditorDraft()
	prevConn, prevDB := m.activeConnID, m.activeDB
	m.stashResultsForTab(prevConn, prevDB)
	m.activeConnID = connID
	m.activeDB = dbName
	m.applyResultsForTab(connID, dbName)
	m.editor.OpenTab(m.connKey(), m.tabLabelForConn(connID, dbName))
	m.pruneTabResultsCache()
	m.persistOpenTabs()
	m.applyEditorSchema(connID, dbName)
	m.aiPane.SetConnKey(m.connKey())
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
		if node.Detail == "export_all_ddl" {
			cmds = append(cmds, m.setStatusPersistent("Exporting all DDLs..."))
			cmds = append(cmds, m.exportAllDDLCmd(node.ConnID, node.DBName))
			return cmds
		}
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
		if node.Detail == "ddl" {
			m.activateExplorerDatabase(node.ConnID, node.DBName)
			isView := node.Kind == explorer.NodeView
			cmds = append(cmds, m.setStatus("Loading DDL..."))
			cmds = append(cmds, m.fetchTableDDLCmd(node.ConnID, node.DBName, node.Label, isView))
			return cmds
		}
		if node.Detail == "export_all_ddl" {
			cmds = append(cmds, m.setStatusPersistent("Exporting all DDLs..."))
			cmds = append(cmds, m.exportAllDDLCmd(node.ConnID, node.DBName))
			return cmds
		}
		m.activateExplorerDatabase(node.ConnID, node.DBName)
		if node.Detail == "select" {
			driver := ""
			if conn := m.findConn(node.ConnID); conn != nil {
				driver = conn.Driver
			}
			query := quickSelectQuery(driver, node.DBName, node.Label)
			if !m.editor.MoveCursorToQueryBlockIfPresent(query) {
				m.editor.AppendAtEnd(query)
				m.persistEditorDraft()
			}
			// Keep focus in explorer; user can press r to view results.
			m.beginQueryForActiveTab()
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

// appendDeleteUpdateDraft appends generated DELETE/UPDATE/INSERT SQL. Inserts a blank
// line before the first draft when the buffer already has content; chains
// further DELETE/UPDATE/INSERT statements without an extra blank line.
func (m *Model) appendDeleteUpdateDraft(sql string) {
	last := strings.ToUpper(strings.TrimSpace(m.editor.LastNonBlankLine()))
	if strings.HasPrefix(last, "UPDATE ") || strings.HasPrefix(last, "DELETE ") || strings.HasPrefix(last, "INSERT ") {
		m.editor.AppendInline(sql)
	} else {
		m.editor.AppendAtEnd(sql)
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

func (m *Model) buildUpdateDraftCmd(msg results.UpdateDraftRequestMsg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		conn := m.findConn(m.activeConnID)
		if conn == nil {
			return results.UpdateDraftMsg{Err: "No active connection."}
		}
		drv, err := db.New(*conn)
		if err != nil {
			return results.UpdateDraftMsg{Err: err.Error()}
		}
		if err := drv.Connect(ctx, *conn); err != nil {
			return results.UpdateDraftMsg{Err: err.Error()}
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
		sqlText, err := sqlutil.UpdateForRow(msg.Driver, msg.TableExpr, msg.Columns, msg.Row, msg.ColName, msg.NewValue, whereCols)
		if err != nil {
			return results.UpdateDraftMsg{Err: err.Error()}
		}
		return results.UpdateDraftMsg{SQL: sqlText}
	}
}

func (m *Model) testConnectionCmd(conn config.Connection) tea.Cmd {
	return func() tea.Msg {
		drv, err := db.New(conn)
		if err != nil {
			return explorer.ConnTestResultMsg{Err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := drv.Connect(ctx, conn); err != nil {
			return explorer.ConnTestResultMsg{Err: err}
		}
		defer drv.Close()
		if err := drv.Ping(ctx); err != nil {
			return explorer.ConnTestResultMsg{Err: err}
		}
		return explorer.ConnTestResultMsg{}
	}
}

func (m *Model) fetchTableDDLCmd(connID, dbName, table string, isView bool) tea.Cmd {
	conn := m.findConn(connID)
	kind := "table"
	if isView {
		kind = "view"
	}
	title := fmt.Sprintf("DDL (%s: %s)", kind, table)
	return func() tea.Msg {
		if conn == nil {
			return tableDDLMsg{title: title, err: fmt.Errorf("no connection")}
		}
		drv, err := db.New(*conn)
		if err != nil {
			return tableDDLMsg{title: title, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := drv.Connect(ctx, *conn); err != nil {
			return tableDDLMsg{title: title, err: err}
		}
		defer drv.Close()
		ddl, err := drv.TableDDL(ctx, dbName, table, isView)
		return tableDDLMsg{title: title, ddl: ddl, err: err}
	}
}

func (m *Model) exportAllDDLCmd(connID, dbName string) tea.Cmd {
	conn := m.findConn(connID)
	return func() tea.Msg {
		if conn == nil {
			return exportAllDDLResultMsg{err: fmt.Errorf("no connection found for ID %q", connID)}
		}
		drv, err := db.New(*conn)
		if err != nil {
			return exportAllDDLResultMsg{err: fmt.Errorf("failed to create driver: %w", err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := drv.Connect(ctx, *conn); err != nil {
			return exportAllDDLResultMsg{err: fmt.Errorf("failed to connect: %w", err)}
		}
		defer drv.Close()

		tables, err := drv.Tables(ctx, dbName)
		if err != nil {
			return exportAllDDLResultMsg{err: fmt.Errorf("failed to list tables: %w", err)}
		}
		views, err := drv.Views(ctx, dbName)
		if err != nil {
			return exportAllDDLResultMsg{err: fmt.Errorf("failed to list views: %w", err)}
		}

		if len(tables) == 0 && len(views) == 0 {
			return exportAllDDLResultMsg{err: fmt.Errorf("no tables or views found in database %q", dbName)}
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("-- DDL Export for %s:%s\n", conn.Name, dbName))
		sb.WriteString(fmt.Sprintf("-- Generated at %s\n", time.Now().Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("-- Tables: %d, Views: %d\n\n", len(tables), len(views)))

		exportedCount := 0
		for _, t := range tables {
			ddl, err := drv.TableDDL(ctx, dbName, t, false)
			if err != nil {
				sb.WriteString(fmt.Sprintf("-- Error fetching DDL for table %q: %v\n\n", t, err))
				continue
			}
			sb.WriteString(ddl)
			if !strings.HasSuffix(strings.TrimSpace(ddl), ";") {
				sb.WriteString(";")
			}
			sb.WriteString("\n\n")
			exportedCount++
		}
		for _, v := range views {
			ddl, err := drv.TableDDL(ctx, dbName, v, true)
			if err != nil {
				sb.WriteString(fmt.Sprintf("-- Error fetching DDL for view %q: %v\n\n", v, err))
				continue
			}
			sb.WriteString(ddl)
			if !strings.HasSuffix(strings.TrimSpace(ddl), ";") {
				sb.WriteString(";")
			}
			sb.WriteString("\n\n")
			exportedCount++
		}

		dir := m.workDir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		if dir == "" {
			dir, _ = os.UserHomeDir()
		}
		if dir == "" {
			dir = "."
		}

		safeConnName := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, conn.Name)
		safeDBName := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, dbName)

		filename := fmt.Sprintf("dbx_export_%s_%s_%d.sql", safeConnName, safeDBName, time.Now().Unix())
		path := filepath.Join(dir, filename)
		absPath, err := filepath.Abs(path)
		if err == nil {
			path = absPath
		}

		if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
			return exportAllDDLResultMsg{err: fmt.Errorf("failed to write file %q: %w", path, err)}
		}

		return exportAllDDLResultMsg{path: path}
	}
}

func (m Model) handleHelpPopupKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	raw := strings.TrimLeft(helpScreenText, "\n")
	lines := strings.Split(raw, "\n")
	visible := m.helpPopupInnerLines()
	maxTop := max(0, len(lines)-visible)
	switch msg.String() {
	case "esc", "enter", "?", "q":
		m.showHelp = false
		m.helpScroll = 0
	case "j", "down":
		if m.helpScroll < maxTop {
			m.helpScroll++
		}
	case "k", "up":
		if m.helpScroll > 0 {
			m.helpScroll--
		}
	case "pgdown":
		m.helpScroll += visible
		if m.helpScroll > maxTop {
			m.helpScroll = maxTop
		}
	case "pgup":
		m.helpScroll -= visible
		if m.helpScroll < 0 {
			m.helpScroll = 0
		}
	case "g":
		m.helpScroll = 0
	case "G":
		m.helpScroll = maxTop
	}
	return m, nil
}

func (m Model) helpPopupInnerLines() int {
	return m.ddlPopupInnerLines()
}

// helpScreenText is the full help document (one screen line per \n).
const helpScreenText = `
  dbx — TUI Database Client

  NAVIGATION
    e           Focus explorer
    q           Focus editor
    r           Focus results
    a           Focus AI pane (shows it if hidden; does not hide — use palette)
    space       Open command palette (context-aware)
    space+e     Editor palette: wrap query for EXPLAIN (driver-specific)
    space+f     Toggle fullscreen for current panel
    ?           Toggle this help

  EXPLORER
    j/k         Navigate up/down
    enter/l     Expand/collapse node (incl. table columns)
    h           Collapse current branch
    s           Append SELECT * … LIMIT/TOP 100, run it (keeps existing editor text)
    v           DDL popup for table/view (CREATE + indexes; driver-specific)
    V           Export all DDLs for database to file
    f           Set filter for tables

  EDITOR (Normal mode)
    tab         Next query tab
    shift+tab   Previous query tab
    i/o         Enter insert mode
    enter       Execute query under cursor (batch: only DELETE/UPDATE split by ; → run in order)
    u           Undo edit (whole insert session until esc; normal edits undo separately)
    ctrl+r      Redo edit
    ctrl+p/n    Browse query history (replace buffer)
    backspace   History popup (type to filter, ↑↓ navigate, Ctrl+d delete)
    dd          Delete line
    dq          Delete query
    dw          Delete current word                                                                                                                                                                                               |
    d$          Delete to end of line                                                                                                                                                                                             |
    d0          Delete to start of line                                                                                                                                                                                           |
    yy          Yank/copy line
    yq          Yank/copy query
    yw          Yank/copy current word                                                                                                                                                                                            |
    y$          Yank/copy to end of line                                                                                                                                                                                             |
    y0          Yank/copy to start of line                                                                                                                                                                                           |
    cc          Clean prefix (numbers, |) from line
    cq          Clean prefix (numbers, |) from query
    gg/G        Go to top/bottom
    J/K         Jump to next/prev query block

  EDITOR (Insert mode)
    esc         Return to normal mode
    tab         Trigger autocomplete
    ctrl+enter  Execute query
    ctrl+r      Execute query

  RESULTS
    j/k         Navigate rows
    pgup/pgdn   Page up/down rows 
    ctrl+d/u    Scroll down / up by half page                                                                                                                                                                                                                                                                                                                                                                                  |
    h/l         Navigate columns
    g/G         First/last row
    s           Toggle mark on current row (for delete/insert drafts)
    S           Mark current row + band-select while moving j/k/g/G (esc clears marks)
    d           Append DELETE draft(s) for marked rows (or cursor row); WHERE uses PK cols when possible
    i           Append INSERT draft(s) for marked rows (or cursor row); VALUES from result columns
    u           Update cell — popup to enter new value, generates UPDATE with PK WHERE
    v           View full cell popup (y copy, f JSON format persists across cells, h/l adjacent col, j/k adjacent row, esc)

  AI ASSISTANT (Normal mode — transcript)
    i           Insert mode (prompt field)
    hjkl/arrows  Move block cursor; mouse wheel scrolls
    J/K         Jump to next / previous fenced sql code block (same idea as editor J/K query jumps)
    enter       Copy SQL from the block under the cursor to the query editor

  AI ASSISTANT (Insert mode — prompt)
    esc         Normal mode (transcript)
    enter       Send prompt, or /clear to wipe transcript and start a new CLI chat session
    /clear      Clear current ai session
    /results    Send with results pane query + grid in context (/results alone uses a default question)
    @ / #       Table / column pickers (DDL for @mentions is added when the prompt is sent)
    alt+enter   New line in the prompt

  COMMAND PALETTE (space, then key)
    Explorer:   n=add connection  e=edit  d=delete  R=refresh  t=toggle explorer  a=toggle AI pane  f=fullscreen
    Editor:     x=execute  e=explain  c=clear  D=close tab (popup: y/enter · n esc q)  t=toggle explorer  a=toggle AI pane  f=fullscreen
    Results:    y=copy cell  Y=copy row  c=copy all (headers)  e=export CSV  j=export JSON  t=toggle explorer  a=toggle AI pane  f=fullscreen
    AI:         t=toggle explorer  a=toggle AI pane  f=fullscreen
`

func (m Model) handleDDLPopupKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	lines := strings.Split(strings.ReplaceAll(m.ddlPopupText, "\r\n", "\n"), "\n")
	// Must match renderDDLPopup inner height (ddlPopupInnerLines is for full-height help overlay).
	visible := m.ddlPopupScrollViewportLines()
	maxTop := max(0, len(lines)-visible)
	switch msg.String() {
	case "esc", "enter", "q":
		m.ddlPopupOpen = false
		m.ddlPopupText = ""
		m.ddlPopupTitle = ""
		m.ddlPopupScroll = 0
	case "y":
		if m.ddlPopupText != "" {
			_ = util.Copy(m.ddlPopupText)
		}
	case "j", "down":
		if m.ddlPopupScroll < maxTop {
			m.ddlPopupScroll++
		}
	case "k", "up":
		if m.ddlPopupScroll > 0 {
			m.ddlPopupScroll--
		}
	case "pgdown":
		m.ddlPopupScroll += visible
		if m.ddlPopupScroll > maxTop {
			m.ddlPopupScroll = maxTop
		}
	case "pgup":
		m.ddlPopupScroll -= visible
		if m.ddlPopupScroll < 0 {
			m.ddlPopupScroll = 0
		}
	case "g":
		m.ddlPopupScroll = 0
	case "G":
		m.ddlPopupScroll = maxTop
	}
	return m, nil
}

func (m Model) ddlPopupInnerLines() int {
	boxH := m.height - 2
	if boxH < 6 {
		boxH = m.height
	}
	innerH := boxH - 2
	if innerH < 1 {
		innerH = 1
	}
	return innerH
}

// ddlPopupScrollViewportLines returns the content row count for the DDL popup; kept in sync with renderDDLPopup.
func (m Model) ddlPopupScrollViewportLines() int {
	boxH := min(m.height-2, max(12, m.height*68/100))
	if boxH < 6 {
		boxH = min(m.height-2, 6)
	}
	innerH := boxH - 2
	if innerH < 1 {
		innerH = 1
	}
	return innerH
}

func (m Model) renderTabCloseConfirmPopup() string {
	boxW := min(max(36, m.width*45/100), m.width-4)
	if boxW < 28 {
		boxW = min(m.width-2, 36)
	}
	if boxW < 24 {
		boxW = m.width
	}
	innerW := boxW - 6
	if innerW < 12 {
		innerW = 12
	}

	label := m.tabLabelForConn(m.activeConnID, m.activeDB)
	if strings.TrimSpace(label) == "" {
		label = m.editor.EditorConnKey()
	}
	if strings.TrimSpace(label) == "" {
		label = "this connection"
	}
	label = ansi.Truncate(label, innerW, "…")

	head := m.theme.Bold.Render("Close this tab?")
	preview := m.theme.Normal.Render(label)
	hint := m.theme.Dimmed.Render("Queries/text is still saved.")
	inner := lipgloss.JoinVertical(lipgloss.Left,
		head,
		"",
		preview,
		"",
		hint,
	)
	body := lipgloss.NewStyle().Width(innerW).Padding(0, 1).Render(inner)
	popup := m.theme.BorderFocused.Width(boxW - 2).Render(body)
	popup = m.embedDDLPopupBorderLabels(popup, "Close tab", "Enter/y: confirm · Esc/n/q: cancel")
	// Do not use lipgloss.Place(full screen): View() composites with overlayCentered, which
	// splices this box onto the existing layout. A full-screen Place buffer would replace every line.
	return popup
}

func (m Model) renderDDLPopup() string {
	// Smaller than the help overlay so more of the app stays visible; still scrollable.
	boxW := min(m.width-4, max(40, m.width*75/100))
	boxH := min(m.height-2, max(12, m.height*68/100))
	if boxW < 20 {
		boxW = min(m.width-2, 20)
	}
	if boxH < 6 {
		boxH = min(m.height-2, 6)
	}
	innerW := boxW - 4
	innerH := boxH - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}

	lines := strings.Split(strings.ReplaceAll(m.ddlPopupText, "\r\n", "\n"), "\n")
	maxTop := max(0, len(lines)-innerH)
	scroll := min(m.ddlPopupScroll, maxTop)

	var sb strings.Builder
	for i := scroll; i < len(lines) && i < scroll+innerH; i++ {
		sb.WriteString(ansi.Truncate(lines[i], innerW, "…"))
		sb.WriteString("\n")
	}
	body := lipgloss.NewStyle().Width(innerW).Height(innerH).Render(sb.String())
	title := m.ddlPopupTitle
	if title == "" {
		title = "DDL"
	}
	footer := "y: copy · j/k  g/G  PgUp/PgDn · Esc: close"
	if maxTop == 0 {
		footer = "y: copy · Esc: close"
	}
	popup := m.theme.BorderFocused.Width(boxW - 2).Height(boxH - 2).Render(body)
	popup = m.embedDDLPopupBorderLabels(popup, title, footer)
	// Return the bordered box only; View() uses overlayCentered (see renderTabCloseConfirmPopup).
	return popup
}

func (m Model) embedDDLPopupBorderLabels(boxed, topLabel, bottomLabel string) string {
	ls := strings.Split(boxed, "\n")
	if len(ls) < 2 {
		return boxed
	}
	width := ansi.StringWidth(ls[0])
	ls[0] = m.renderDDLPopupBorderLine(width, topLabel, true)
	ls[len(ls)-1] = m.renderDDLPopupBorderLine(width, bottomLabel, false)
	return strings.Join(ls, "\n")
}

func (m Model) renderDDLPopupBorderLine(width int, label string, top bool) string {
	if width < 3 {
		return strings.Repeat("─", max(0, width))
	}
	left, right := "╭", "╮"
	if !top {
		left, right = "╰", "╯"
	}
	sep := "─"
	mid := width - 2
	part := sep + " " + label + " "
	if ansi.StringWidth(part) > mid {
		inner := mid - ansi.StringWidth(sep+"  ")
		if inner < 1 {
			part = strings.Repeat(sep, mid)
		} else {
			part = sep + " " + ansi.Truncate(label, inner, "…") + " "
		}
	}
	fill := max(0, mid-ansi.StringWidth(part))
	var line string
	if top {
		line = left + part + strings.Repeat(sep, fill) + right
	} else {
		line = left + strings.Repeat(sep, fill) + part + right
	}
	st := lipgloss.NewStyle().Foreground(m.theme.BorderFocused.GetBorderTopForeground())
	return st.Render(line)
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
				connID:    connID,
				dbName:    dbName,
			}
		}
		driver, err := db.New(*conn)
		if err != nil {
			return dbQueryResultMsg{
				result:    &db.QueryResult{Error: err.Error()},
				sourceSQL: query,
				connID:    connID,
				dbName:    dbName,
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := driver.Connect(ctx, *conn); err != nil {
			return dbQueryResultMsg{
				result:    &db.QueryResult{Error: err.Error()},
				sourceSQL: query,
				connID:    connID,
				dbName:    dbName,
			}
		}
		defer driver.Close()

		if stmts, ok := sqlutil.SplitExecBatchDeleteUpdate(query); ok && len(stmts) > 1 {
			return execDeleteUpdateBatch(ctx, driver, connID, dbName, stmts, start, query)
		}

		// Determine if SELECT or DML
		trimmed := strings.TrimSpace(strings.ToUpper(query))
		var result *db.QueryResult
		if strings.HasPrefix(trimmed, "SELECT") || strings.HasPrefix(trimmed, "WITH") ||
			strings.HasPrefix(trimmed, "SHOW") || strings.HasPrefix(trimmed, "EXPLAIN") ||
			strings.HasPrefix(trimmed, "DESCRIBE") || strings.HasPrefix(trimmed, "DESC") ||
			strings.HasPrefix(trimmed, "SET SHOWPLAN_ALL ON") {
			result, err = driver.Query(ctx, dbName, query)
		} else {
			result, err = driver.Exec(ctx, dbName, query)
		}
		if err != nil {
			result = &db.QueryResult{Error: err.Error()}
		}
		if result == nil {
			result = &db.QueryResult{}
		}
		return dbQueryResultMsg{
			result:    result,
			elapsed:   time.Since(start),
			sourceSQL: query,
			connID:    connID,
			dbName:    dbName,
		}
	}
}

// execDeleteUpdateBatch runs several DELETE/UPDATE statements sequentially (drivers often
// reject multiple statements in a single Exec call).
func execDeleteUpdateBatch(ctx context.Context, driver db.Driver, connID, dbName string, stmts []string, start time.Time, sourceSQL string) dbQueryResultMsg {
	var rows [][]string
	for i, stmt := range stmts {
		result, err := driver.Exec(ctx, dbName, stmt)
		if err != nil {
			return dbQueryResultMsg{
				result:    &db.QueryResult{Error: fmt.Sprintf("statement %d: %v", i+1, err)},
				elapsed:   time.Since(start),
				sourceSQL: sourceSQL,
				connID:    connID,
				dbName:    dbName,
			}
		}
		if result != nil && result.Error != "" {
			return dbQueryResultMsg{
				result:    &db.QueryResult{Error: fmt.Sprintf("statement %d: %s", i+1, result.Error)},
				elapsed:   time.Since(start),
				sourceSQL: sourceSQL,
				connID:    connID,
				dbName:    dbName,
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
		connID:    connID,
		dbName:    dbName,
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
	case "mongodb":
		return fmt.Sprintf(`{"find": "%s", "limit": 100}`, table)
	case "orientdb":
		return "SELECT FROM " + table + " LIMIT 100"
	case "oracle":
		if database != "" {
			return "SELECT * FROM \"" + database + "\".\"" + table + "\" FETCH FIRST 100 ROWS ONLY"
		}
		return "SELECT * FROM \"" + table + "\" FETCH FIRST 100 ROWS ONLY"
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

// setStatusPersistent sets a status message that does not expire.
func (m *Model) setStatusPersistent(msg string) tea.Cmd {
	m.statusMsg = msg
	m.statusExpiry = time.Time{}
	return nil
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

func (m *Model) persistPaneVisibility() {
	if m.cfg.Layout.ExplorerHidden == nil {
		v := false
		m.cfg.Layout.ExplorerHidden = &v
	}
	if m.cfg.Layout.AIPaneHidden == nil {
		v := true
		m.cfg.Layout.AIPaneHidden = &v
	}
	*m.cfg.Layout.ExplorerHidden = m.explorerHidden
	*m.cfg.Layout.AIPaneHidden = m.aiHidden
	_ = config.Save(m.cfg)
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
		m.aiPane.SetSize(m.width-2, totalH-2)
		return
	}

	explorerW := m.width * m.cfg.Layout.ExplorerWidthPct / 100
	if m.explorerHidden {
		explorerW = 0
	}
	aiW := 0
	if !m.aiHidden {
		aiW = m.width * m.cfg.Layout.AIPaneWidthPct / 100
		if aiW < 4 {
			aiW = 4
		}
	}
	midW := m.width - explorerW - aiW
	if midW < 2 {
		midW = 2
	}

	editorH := totalH * m.cfg.Layout.EditorHeightPct / 100
	resultsH := totalH - editorH

	explorerInner := explorerW - 2
	if explorerInner < 0 {
		explorerInner = 0
	}
	m.explorer.SetSize(explorerInner, totalH-2)
	m.editor.SetSize(midW-2, editorH-2)
	m.results.SetSize(midW-2, resultsH-2)
	if !m.aiHidden {
		m.aiPane.SetSize(aiW-2, totalH-2)
	}
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.fullscreenOn {
		content := m.renderPanelContent(m.fullscreenPanel)
		boxed := m.theme.BorderFocused.Width(m.width - 2).Height(m.height - 3).Render(content)
		border := m.embedPanelTopTitle(boxed, m.panelTitleFor(m.fullscreenPanel), true)
		border = m.embedPanelBottomHint(border, m.panelBottomHintFor(m.fullscreenPanel), true)
		view := lipgloss.JoinVertical(lipgloss.Left, border, m.renderStatusBar())
		if m.showHelp {
			view = overlayCentered(view, m.renderHelp(), m.width, m.height)
		}
		if m.ddlPopupOpen {
			view = overlayCentered(view, m.renderDDLPopup(), m.width, m.height)
		}
		if m.tabCloseConfirm {
			view = overlayCentered(view, m.renderTabCloseConfirmPopup(), m.width, m.height)
		}
		return view
	}

	explorerView := ""
	if !m.explorerHidden {
		explorerView = m.renderBorderedPanel(PanelExplorer)
	}

	editorView := m.renderBorderedPanel(PanelEditor)
	resultsView := m.renderBorderedPanel(PanelResults)

	rightPane := lipgloss.JoinVertical(lipgloss.Left, editorView, resultsView)

	aiView := ""
	if !m.aiHidden {
		aiView = m.renderBorderedPanel(PanelAI)
	}

	var mainView string
	if m.explorerHidden {
		mainView = rightPane
	} else {
		mainView = lipgloss.JoinHorizontal(lipgloss.Top, explorerView, rightPane)
	}

	if !m.aiHidden {
		mainView = lipgloss.JoinHorizontal(lipgloss.Top, mainView, aiView)
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

	if m.ddlPopupOpen {
		view = overlayCentered(view, m.renderDDLPopup(), m.width, m.height)
	}

	if m.tabCloseConfirm {
		popup := m.renderTabCloseConfirmPopup()
		exW := 0
		if explorerView != "" {
			if ls := strings.Split(explorerView, "\n"); len(ls) > 0 {
				exW = lipgloss.Width(ls[0])
			}
		}
		edLines := strings.Split(editorView, "\n")
		edH := len(edLines)
		edW := 0
		for _, ln := range edLines {
			if w := lipgloss.Width(ln); w > edW {
				edW = w
			}
		}
		view = overlayInEditorPane(view, popup, m.width, exW, 0, edW, edH)
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
	case PanelAI:
		return m.aiPane.View()
	}
	return ""
}

// rightColumnWidth returns the bordered width for editor/results (matches layoutPanels right split).
func (m Model) rightColumnWidth() int {
	explorerW := m.width * m.cfg.Layout.ExplorerWidthPct / 100
	if m.explorerHidden {
		explorerW = 0
	}
	aiW := 0
	if !m.aiHidden {
		aiW = m.width * m.cfg.Layout.AIPaneWidthPct / 100
		if aiW < 4 {
			aiW = 4
		}
	}
	midW := m.width - explorerW - aiW
	w := midW - 2
	if w < 0 {
		w = 0
	}
	return w
}

func (m Model) panelBottomHintFor(p Panel) string {
	switch p {
	case PanelExplorer:
		return "f: filter · s: show rows · v: DDL · V: Export All · space: commands"
	case PanelEditor:
		if m.editor.IsInsertMode() {
			return "Esc: normal mode · Tab: autocomplete · Ctrl+Enter/Ctrl+r: run query"
		}
		return "Enter: run query · Tab: next tab · Sh-Tab: prev tab · i: insert · d: delete · y: yank/copy · backspace: history · space: commands"
	case PanelResults:
		return "h/l, 0/$, PgUp/PgDn: movement, s: toggle row mark · S: band select rows · d: delete draft · i: insert draft · u: update cell · v: full cell · space: commands"
	case PanelAI:
		return "?: help · i: insert mode · esc: normal mode"
	default:
		return ""
	}
}

func (m Model) panelTitleFor(p Panel) string {
	switch p {
	case PanelExplorer:
		return "[e] Explorer"
	case PanelEditor:
		return "[q] Query Editor "
	case PanelResults:
		return "[r] Results"
	case PanelAI:
		return "[a] AI Assistant"
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

// embedPanelBottomHint replaces the last border line with a titled bottom edge (hint on the right).
func (m Model) embedPanelBottomHint(boxed, hint string, focused bool) string {
	if hint == "" {
		return boxed
	}
	lines := strings.Split(boxed, "\n")
	if len(lines) < 2 {
		return boxed
	}
	tw := ansi.StringWidth(lines[len(lines)-1])
	lines[len(lines)-1] = m.renderPanelBottomBorderLine(tw, hint, focused)
	return strings.Join(lines, "\n")
}

func (m Model) renderPanelBottomBorderLine(outerWidth int, label string, focused bool) string {
	if outerWidth < 3 {
		return strings.Repeat("─", max(0, outerWidth))
	}
	tl := "╰"
	tr := "╯"
	sep := "─"
	mid := outerWidth - ansi.StringWidth(tl) - ansi.StringWidth(tr)
	if mid < 1 {
		return m.styleForPanelTopLine(focused).Render(tl + strings.Repeat(sep, max(0, mid)) + tr)
	}
	rightPart := sep + " " + label + " "
	rpw := ansi.StringWidth(rightPart)
	if rpw > mid {
		inner := mid - ansi.StringWidth(sep+"  ")
		if inner < 1 {
			rightPart = strings.Repeat(sep, mid)
		} else {
			rightPart = sep + " " + ansi.Truncate(label, inner, "…") + " "
			rpw = ansi.StringWidth(rightPart)
		}
	}
	fill := mid - rpw
	if fill < 0 {
		fill = 0
	}
	line := tl + strings.Repeat(sep, fill) + rightPart + tr
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
	hint := m.panelBottomHintFor(p)
	switch p {
	case PanelExplorer:
		w := m.width*m.cfg.Layout.ExplorerWidthPct/100 - 2
		h := totalH - 2
		if w < 0 {
			w = 0
		}
		boxed := borderStyle.Width(w).Height(h).Render(content)
		boxed = m.embedPanelTopTitle(boxed, title, focused)
		return m.embedPanelBottomHint(boxed, hint, focused)
	case PanelEditor:
		w := m.rightColumnWidth()
		h := totalH*m.cfg.Layout.EditorHeightPct/100 - 2
		if h < 0 {
			h = 0
		}
		boxed := borderStyle.Width(w).Height(h).Render(content)
		boxed = m.embedPanelTopTitle(boxed, title, focused)
		return m.embedPanelBottomHint(boxed, hint, focused)
	case PanelResults:
		w := m.rightColumnWidth()
		h := totalH*(100-m.cfg.Layout.EditorHeightPct)/100 - 2
		if h < 0 {
			h = 0
		}
		boxed := borderStyle.Width(w).Height(h).Render(content)
		boxed = m.embedPanelTopTitle(boxed, title, focused)
		return m.embedPanelBottomHint(boxed, hint, focused)
	case PanelAI:
		aiW := m.width * m.cfg.Layout.AIPaneWidthPct / 100
		if aiW < 4 {
			aiW = 4
		}
		w := aiW - 2
		h := totalH - 2
		if w < 0 {
			w = 0
		}
		boxed := borderStyle.Width(w).Height(h).Render(content)
		boxed = m.embedPanelTopTitle(boxed, title, focused)
		return m.embedPanelBottomHint(boxed, hint, focused)
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
		status = "  [" + m.focus.String() + "]  " + connInfo + " · Commands: space · Focus: e/q/r · Help: ? · Quit: ctrl+c"
	}
	return m.theme.StatusBar.Width(m.width).Render(status)
}

func (m Model) renderHelp() string {
	boxW := m.width - 4
	boxH := m.height - 2
	if boxW < 20 {
		boxW = m.width
	}
	if boxH < 6 {
		boxH = m.height
	}
	innerW := boxW - 4
	innerH := boxH - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}

	raw := strings.TrimLeft(helpScreenText, "\n")
	lines := strings.Split(raw, "\n")
	maxTop := max(0, len(lines)-innerH)
	scroll := min(m.helpScroll, maxTop)

	var sb strings.Builder
	for i := scroll; i < len(lines) && i < scroll+innerH; i++ {
		sb.WriteString(ansi.Truncate(lines[i], innerW, "…"))
		sb.WriteString("\n")
	}
	body := lipgloss.NewStyle().Width(innerW).Height(innerH).Render(sb.String())

	footerKey := "j/k  g/G  PgUp/PgDn  ?/esc/q close"
	var footer string
	if maxTop == 0 {
		footer = footerKey
	} else {
		end := min(scroll+innerH, len(lines))
		footer = fmt.Sprintf("%d–%d / %d  ·  %s", scroll+1, end, len(lines), footerKey)
	}

	popup := m.theme.BorderFocused.Width(boxW - 2).Height(boxH - 2).Render(body)
	popup = m.embedDDLPopupBorderLabels(popup, "dbx — Help", footer)
	// Return the bordered box only; View() uses overlayCentered (see renderTabCloseConfirmPopup).
	return popup
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

// overlayInEditorPane places the overlay centered in the query-editor rectangle (rx, ry, rw, rh),
// with a slight upward bias so it sits a bit above the pane’s vertical midpoint.
func overlayInEditorPane(base, overlay string, width, rx, ry, rw, rh int) string {
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")
	overlayH := len(overlayLines)
	overlayW := 0
	for _, l := range overlayLines {
		if lw := lipgloss.Width(l); lw > overlayW {
			overlayW = lw
		}
	}
	startRow := ry + (rh-overlayH)/2
	if startRow > ry {
		startRow--
	}
	startCol := rx + (rw-overlayW)/2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}
	if rh > 0 && overlayH > rh {
		startRow = ry
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
