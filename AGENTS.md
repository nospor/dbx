# dbx — AI Agent Context

**dbx** is a TUI (terminal) database client written in Go. It connects to PostgreSQL, MySQL, SQLite, and MSSQL, with a three-panel layout: Explorer | Query Editor | Results.

## Tech Stack

- **Go 1.22+** — main language
- **Bubble Tea** — TUI framework (Elm architecture)
- **Lipgloss** — styling and layout
- **Bubbles** — reusable components (viewport, table, textinput)
- **Chroma** — SQL syntax highlighting
- **Database drivers** — pgx (Postgres), go-sql-driver/mysql, modernc.org/sqlite, microsoft/go-mssqldb

## Project Structure

```
dbx/
├── main.go                 # Entry point; loads config, starts Bubble Tea
├── internal/
│   ├── app/                # Root model, focus, keymap, message routing
│   ├── config/             # Config load/save (~/.config/dbx/config.json)
│   ├── db/                 # Driver interface + Postgres/MySQL/SQLite/MSSQL impls
│   ├── history/            # Executed query history (~/.config/dbx/history.json)
│   ├── querycontents/      # Editor buffer drafts per conn/db (~/.config/dbx/query-contents.json)
│   ├── ui/
│   │   ├── cmdpalette/     # Space-triggered command palette
│   │   ├── editor/         # Query editor (vim mode, syntax highlight, autocomplete)
│   │   ├── explorer/       # Tree view, connection form, conn CRUD
│   │   ├── results/        # Table view, copy, export, horizontal scroll
│   │   └── theme/          # Light/dark/terminal themes
│   └── util/               # Clipboard (copy/paste)
├── spec.md                 # Original feature spec
└── README.md               # User-facing docs
```

## Architecture

- **Root model** (`internal/app/app.go`): Composes explorer, editor, results, palette. Handles global keys (e/focus, space/palette, ?/help), routes messages to focused panel, manages DB connections and async query execution.
- **Async DB ops**: All DB work is non-blocking via `tea.Cmd` and custom message types in `internal/app/messages.go`.
- **Vim mode**: Editor has Normal/Insert modes; motions and commands in `internal/ui/editor/vim.go` and `editor.go`.
- **Multi-query**: Queries separated by blank lines; the one under the cursor is executed.

## Conventions

- Config, query history, and per-database **editor drafts** (`query-contents.json`) live in `~/.config/dbx/`. Drafts persist on `esc` (Insert→Normal) and when changing the active database; they are independent of `history.json`.
- Connection form uses a driver selector (not free text); SQLite hides host/port fields.
- Quick SELECT (`s` on table) appends a dialect-aware query at the end of the editor, then runs it (does not replace the buffer).
- Results pane supports horizontal scrolling with `h`/`l`, `0`/`$`; scrollbar thumb reflects position.

## Build & Run

```bash
go build -o dbx .
./dbx
```

See `README.md` for keybindings and configuration.
