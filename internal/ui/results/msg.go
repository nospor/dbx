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
