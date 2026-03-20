package editor

const maxUndoDepth = 200

type editorSnapshot struct {
	lines []string
	row   int
	col   int
}

type tabUndoState struct {
	undo []editorSnapshot
	redo []editorSnapshot
}

func cloneLines(lines []string) []string {
	if len(lines) == 0 {
		return []string{""}
	}
	out := make([]string, len(lines))
	copy(out, lines)
	return out
}

func (m *Model) tabUndoKey() string {
	return tabStoreKey(m.connKey)
}

func (m *Model) ensureTabUndo() *tabUndoState {
	key := m.tabUndoKey()
	if m.tabUndo == nil {
		m.tabUndo = make(map[string]*tabUndoState)
	}
	if m.tabUndo[key] == nil {
		m.tabUndo[key] = &tabUndoState{}
	}
	return m.tabUndo[key]
}

func (m *Model) clearTabUndo(key string) {
	if m.tabUndo == nil {
		return
	}
	delete(m.tabUndo, key)
}

// beforeInsertEdit starts an undo group for the current insert session: the first
// mutating edit in insert mode records a checkpoint; further edits until Esc share it.
func (m *Model) beforeInsertEdit() {
	if m.skipUndoRecord || m.vim.mode != ModeInsert || m.insertUndoSeeded {
		return
	}
	m.pushUndoPoint()
	m.insertUndoSeeded = true
}

// pushUndoPoint saves the current buffer + cursor before a mutating edit.
// Clears the redo branch. No-op during undo/redo application or when skipped.
func (m *Model) pushUndoPoint() {
	if m.skipUndoRecord {
		return
	}
	st := m.ensureTabUndo()
	st.undo = append(st.undo, m.captureSnapshot())
	if len(st.undo) > maxUndoDepth {
		st.undo = append([]editorSnapshot{}, st.undo[len(st.undo)-maxUndoDepth:]...)
	}
	st.redo = st.redo[:0]
}

func (m *Model) captureSnapshot() editorSnapshot {
	return editorSnapshot{
		lines: cloneLines(m.lines()),
		row:   m.vim.row,
		col:   m.vim.col,
	}
}

func (m *Model) applySnapshot(s editorSnapshot) {
	lines := cloneLines(s.lines)
	if len(lines) == 0 {
		lines = []string{""}
	}
	m.setLines(lines)
	m.vim.row = s.row
	m.vim.col = s.col
	m.compVisible = false
	m.clampCursor()
	m.adjustScroll()
}

// Undo restores the previous buffer state (normal mode: key 'u').
func (m *Model) Undo() bool {
	if m.tabUndo == nil {
		return false
	}
	st := m.tabUndo[m.tabUndoKey()]
	if st == nil || len(st.undo) == 0 {
		return false
	}

	m.skipUndoRecord = true
	defer func() { m.skipUndoRecord = false }()

	cur := m.captureSnapshot()
	st.redo = append(st.redo, cur)

	snap := st.undo[len(st.undo)-1]
	st.undo = st.undo[:len(st.undo)-1]
	m.applySnapshot(snap)
	return true
}

// Redo reapplies a change after undo (normal mode: ctrl+r).
func (m *Model) Redo() bool {
	if m.tabUndo == nil {
		return false
	}
	st := m.tabUndo[m.tabUndoKey()]
	if st == nil || len(st.redo) == 0 {
		return false
	}

	m.skipUndoRecord = true
	defer func() { m.skipUndoRecord = false }()

	cur := m.captureSnapshot()
	st.undo = append(st.undo, cur)
	if len(st.undo) > maxUndoDepth {
		st.undo = append([]editorSnapshot{}, st.undo[len(st.undo)-maxUndoDepth:]...)
	}

	snap := st.redo[len(st.redo)-1]
	st.redo = st.redo[:len(st.redo)-1]
	m.applySnapshot(snap)
	return true
}
