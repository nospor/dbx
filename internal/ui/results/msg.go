package results

// DeleteDraftMsg asks the app to append generated DELETE statement(s) to the query editor.
type DeleteDraftMsg struct {
	SQL string
	Err string
}

// DeleteDraftRequestMsg asks the app to introspect primary keys and build DELETE text.
type DeleteDraftRequestMsg struct {
	Driver    string
	Database  string
	TableExpr string
	Columns   []string
	Rows      [][]string
}

// UpdateDraftMsg carries the generated UPDATE statement (or error) back from the app.
type UpdateDraftMsg struct {
	SQL string
	Err string
}

// InsertDraftMsg asks the app to append generated INSERT statement(s) to the query editor.
type InsertDraftMsg struct {
	SQL string
	Err string
}

// UpdateDraftRequestMsg asks the app to introspect PKs and build an UPDATE statement.
type UpdateDraftRequestMsg struct {
	Driver    string
	Database  string
	TableExpr string
	Columns   []string
	Row       []string
	ColName   string // column being updated
	NewValue  string // new value entered by the user
}

// CopyCellMsg requests the app to copy the selected cell (or error message) to the clipboard.
type CopyCellMsg struct{}

// CopyRowMsg requests the app to copy the selected row (or error message) to the clipboard.
type CopyRowMsg struct{}

// CopyAllRowsMsg requests the app to copy all rows (or error message) to the clipboard.
type CopyAllRowsMsg struct{}

