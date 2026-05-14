package app

import (
	"time"

	"github.com/robertn/dbx/internal/db"
	"github.com/robertn/dbx/internal/ui/explorer"
)

// tableDDLMsg carries CREATE TABLE/VIEW-style DDL from introspection.
type tableDDLMsg struct {
	title string
	ddl   string
	err   error
}

// dbQueryResultMsg carries the result of an async query.
type dbQueryResultMsg struct {
	result    *db.QueryResult
	elapsed   time.Duration
	sourceSQL string // statement that was executed (for results delete drafts)
	connID    string  // connection/db the query ran under (for per-tab results)
	dbName    string
}

// dbSchemaMsg carries the result of an async schema fetch.
type dbSchemaMsg struct {
	connID  string
	dbName  string
	tables  []string
	views   []string
	// allColumns is loaded in the same fetch for editor autocomplete (no need to expand each table).
	allColumns []db.TableColumn
	err        error
}

// dbDatabasesMsg carries the list of databases for a connection.
type dbDatabasesMsg struct {
	connID    string
	databases []string
	err       error
}

// dbColumnsMsg carries column info for a table.
type dbColumnsMsg struct {
	connID  string
	dbName  string
	table   string
	columns []db.ColumnInfo
	err     error
}

// explorerSelectMsg is sent when the user selects a connection/database in explorer.
type explorerSelectMsg struct {
	node *explorer.Node
}

// toggleExplorerMsg shows/hides the explorer pane.
type toggleExplorerMsg struct{}

// toggleAIPaneMsg shows/hides the AI pane.
type toggleAIPaneMsg struct{}

// toggleFullscreenMsg toggles fullscreen for the current panel.
type toggleFullscreenMsg struct{}

// addConnMsg opens the add-connection form.
type addConnMsg struct{}

// editConnMsg opens the edit-connection form for the selected connection.
type editConnMsg struct{}

// deleteConnMsg deletes the selected connection.
type deleteConnMsg struct{}

// refreshSchemaMsg refreshes the schema for the selected database.
type refreshSchemaMsg struct{}

// execQueryFromPaletteMsg executes the current query from the palette.
type execQueryFromPaletteMsg struct{}

// explainQueryFromPaletteMsg wraps the current query for EXPLAIN via the editor palette.
type explainQueryFromPaletteMsg struct{}

// clearEditorMsg clears the current editor buffer.
type clearEditorMsg struct{}

// copyCellMsg copies the selected cell to clipboard.
type copyCellMsg struct{}

// copyRowMsg copies the selected row to clipboard.
type copyRowMsg struct{}

// exportCSVMsg exports results to CSV.
type exportCSVMsg struct{}

// exportJSONMsg exports results to JSON.
type exportJSONMsg struct{}

// exportAllDDLMsg triggers exporting all DDLs for a database.
type exportAllDDLMsg struct {
	connID string
	dbName string
}

// exportAllDDLResultMsg carries the result of exporting all DDLs.
type exportAllDDLResultMsg struct {
	path string
	err  error
}

// exportAllDDLFromPaletteMsg triggers exporting all DDLs from the palette.
type exportAllDDLFromPaletteMsg struct{}

// fetchTableDDLFromPaletteMsg triggers fetching DDL for the selected table from the palette.
type fetchTableDDLFromPaletteMsg struct{}

// aiPreparedPromptMsg carries the full AI prompt after @-mention DDL has been fetched.
type aiPreparedPromptMsg struct {
	connKey    string
	fullPrompt string
}

// closeTabPromptMsg asks to close the current editor tab (confirmation follows).
type closeTabPromptMsg struct{}
