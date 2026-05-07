# dbx — AI Agent Context

**dbx** is a TUI (terminal) database client written in Go. It connects to PostgreSQL, MySQL, SQLite, MSSQL, and MongoDB, with a main three-panel layout: Explorer | Query Editor | Results, plus an **optional AI assistant** column toggled from the UI.

## Tech Stack

- **Go 1.22+** — main language
- **Bubble Tea** — TUI framework (Elm architecture)
- **Lipgloss** — styling and layout
- **Bubbles** — reusable components (viewport, table, textinput)
- **Chroma** — SQL syntax highlighting
- **Database drivers** — pgx (Postgres), go-sql-driver/mysql, modernc.org/sqlite, microsoft/go-mssqldb, go.mongodb.org/mongo-driver/v2 (MongoDB)

## Project Structure

```
dbx/
├── main.go                 # Entry point; loads config, starts Bubble Tea
├── internal/
│   ├── app/                # Root model, focus, keymap, message routing
│   ├── config/             # Config load/save (~/.config/dbx/config.json)
│   ├── db/                 # Driver interface + Postgres/MySQL/SQLite/MSSQL/MongoDB impls
│   ├── ai/                 # AI session store + CLI integration (Ask, persistence)
│   ├── history/            # Executed query history (~/.cache/dbx/history.json)
│   ├── querycontents/      # Editor buffer drafts per conn/db (~/.cache/dbx/query-contents.json)
│   ├── ui/
│   │   ├── ai/             # AI pane (chat transcript, input, @/# overlays, output cursor)
│   │   ├── cmdpalette/     # Space-triggered command palette
│   │   ├── editor/         # Query editor (vim mode, syntax highlight, autocomplete)
│   │   ├── explorer/       # Tree view, connection form, conn CRUD
│   │   ├── results/        # Table view, copy, export, horizontal scroll
│   │   └── theme/          # Light/dark/terminal themes
│   └── util/               # Clipboard (copy/paste)
└── README.md               # User-facing docs
```

## Architecture

- **Root model** (`internal/app/app.go`): Composes explorer, editor, results, AI pane, palette. Handles global keys (e/q/r/a/focus, space/palette, ?/help), routes messages to focused panel, manages DB connections and async query execution.
- **Command palette** (`space` then key): Explorer uses **`n`** add connection, **`a`** toggle AI pane; Editor/Results/AI palettes also include **`a`** toggle AI. In-app help (`?`) lists the full set per panel.
- **AI assistant** (`internal/ui/ai/`, `internal/ai/store.go`): Optional right column; per `connection:database` chat persisted to `~/.cache/dbx/ai_sessions.json`. Prompts run an external CLI from `config.AI` (default profile targets `cursor-agent`). **Normal** mode shows a block cursor on the transcript (reverse video, like the query editor); **Insert** (`i`) uses the textarea; `J`/`K` jump to the next/previous fenced `sql` block (same idea as editor query jumps); `enter` in Normal copies the fenced `sql` block at the cursor into the query editor (`ExtractSQLMsg`). `@` / `#` in Insert open schema table/column overlays. AI responses are async (`AIResponseMsg` always routed to the AI pane).
- **Async DB ops**: All DB work is non-blocking via `tea.Cmd` and custom message types in `internal/app/messages.go`.
- **Vim mode**: Editor has Normal/Insert modes; motions and commands in `internal/ui/editor/vim.go` and `editor.go`.
- **Multi-query**: Queries separated by blank lines; the one under the cursor is executed.

## Conventions

- UI preferences (`config.json`) live under `~/.config/dbx/`. Query history, editor drafts (`query-contents.json`), and AI sessions (`ai_sessions.json`) live in `~/.cache/dbx/`. Drafts persist on `esc` (Insert→Normal) and when changing the active database; they are independent of `history.json`.
- Connection form uses a driver selector (not free text); SQLite hides host/port fields.
- Quick SELECT (`s` on table) appends a dialect-aware query at the end of the editor, then runs it (does not replace the buffer).
- Explorer `v` on a table/view loads driver-specific DDL (CREATE TABLE/VIEW + indexes where supported) in a centered popup.
- Results pane supports horizontal scrolling with `h`/`l`, `0`/`$`; scrollbar thumb reflects position. Pressing `d`/`i`/`u` generates driver-specific drafts (SQL for relational, JSON commands for MongoDB) into the editor.
- AI pane width is `layout.ai_pane_width_pct` in config (default 25). AI CLI apps and `max_history_size_kb` live under `ai` in `config.json`.

## Build & Run

```bash
go build -o dbx .
./dbx
```

See `README.md` for keybindings and configuration.
