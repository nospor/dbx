# Dbx — TUI Database Client

A terminal-based database client written in Go with vim-mode editing, multi-database support, and a command palette.

## Features

- **Databases**: PostgreSQL, MySQL, SQLite, MSSQL
- **Three-panel layout**: Explorer | Query Editor | Results — each pane shows **key hints** on the bottom border (long lines truncate if the pane is narrow)
- **AI assistant** (optional right column, **experimental** for now): chat with a configured CLI (e.g. `cursor-agent`), per connection/database; transcript with **Normal** mode cursor (reverse-video cell like the query editor) and **Insert** for prompts; in Insert, `enter` sends the prompt and `alt+enter` inserts a newline; `enter` in Normal copies the latest AI SQL from a fenced `sql` code block to the query editor; `@` / `#` insert table/column names from schema; **`/results`** sends the prompt together with the results pane’s last successful query and its grid (see **AI Assistant** below)
- **Vim mode**: Normal and Insert mode with motions (h/j/k/l, w/b, gg/G, dd, etc.)
- **Multi-query support**: Separate queries by blank lines; execute the one under the cursor
- **SQL syntax highlighting** via chroma
- **Tab autocomplete** for table names, column names, and SQL keywords (columns are loaded automatically when you expand a database / refresh schema—no need to open each table first)
- **Query history** per connection/database — `ctrl+p` / `ctrl+n` replace the buffer with older/newer entries; `backspace` (normal mode) opens a **filterable** popup — stored in `history.json`
- **Per-database editor drafts** — the query pane is remembered per connection/database; drafts save when you leave Insert mode (`esc`) and when switching databases; stored in `query-contents.json` (separate from history)
- **Query tabs** — each connection/database opens as a tab in the query editor (`tab` / `shift+tab` to cycle; command palette `D` opens a **centered confirm popup** — `y` or `enter` to close, `n` / `esc` / `q` to cancel). Open tabs are restored on startup (`open-tabs.json`)
- **Command palette** (**space**, then a letter) with context-aware commands per panel (`n` add connection in explorer; `a` toggles AI pane from explorer / editor / results / AI)
- **Fullscreen** any panel with space+f
- **Export** results to CSV or JSON
- **Copy** cell or row to clipboard
- **Themes**: terminal (default), dark, light, catppuccin-mocha, catppuccin-latte, nord, gruvbox-dark — configurable in config file

## Install

```bash
git clone ...
cd dbx

# to quickly build
go build -o dbx .

# or (builds slower, but binary is smaller)
go build -trimpath -ldflags="-s -w" -o dbx .

# then run
./dbx

# you may also want to copy the binary to your PATH (and run it from any place), e.g.:
sudo cp dbx /usr/local/bin/
```


## Configuration

Config is stored in `~/.config/dbx/config.json` (UI settings only):

```json
{
  "layout": {
    "explorer_width_pct": 25,
    "editor_height_pct": 50,
    "ai_pane_width_pct": 25
  },
  "theme": "catppuccin-mocha",
  "status_message_seconds": 5,
  "ai": {
    "selected_app": "cursor-agent",
    "max_history_size_kb": 1024,
    "max_results_context_kb": 256,
    "disable_agent_workdir": false,
    "agent_workdir": "",
    "apps": {
      "cursor-agent": {
        "models_command": "cursor-agent models",
        "models_response_format": "Available models\n\n{models}\n\nTip: use --model <id> (or /model <id> in interactive mode) to switch.",
        "create_session_command": "cursor-agent create-chat",
        "session_mode_flag": "--mode ask",
        "resume_session_flag": "--resume",
        "model_flag": "--model"
      }
    }
  }
}
```

Omit `ai` to keep defaults. The `apps` map defines named profiles; `selected_app` must match one key. Each profile supplies shell commands and flags dbx uses when spawning the CLI.

Within `ai`:

- **`max_history_size_kb`** — soft cap for transcript size warnings / history handling for the AI integration.
- **`max_results_context_kb`** — maximum size (in KB) of the **query + result rows** block appended to the outbound prompt when you use **`/results`** in the AI pane (default **256** if omitted or zero). Long SQL is truncated inside the block; rows are cut off with a truncation note if the limit is reached.
- **`disable_agent_workdir`** — when **`false`** (default), dbx runs the AI CLI with **`cmd.Dir`** set to **`agent_workdir`** (or, if **`agent_workdir`** is empty, **`$XDG_CACHE_HOME/dbx/aiagentfolder`**, falling back to **`~/.cache/dbx/aiagentfolder`**). The directory is created if it does not exist. This avoids launching tools like `cursor-agent` from a large repo directory (fewer index/workspace reads). When **`true`**, the AI process inherits dbx’s working directory (same behavior as older dbx releases).
- **`agent_workdir`** — absolute or relative path for that isolated working directory. Leading **`~/`** is expanded to your home directory; **`$VAR`** environment expansion is applied. If empty and **`disable_agent_workdir`** is **`false`**, the default cache path above is used.

Available `theme` values: `terminal`, `dark`, `light`, `catppuccin-mocha`, `catppuccin-latte`, `nord`, `gruvbox-dark`.

Connections are stored separately in `~/.cache/dbx/connections.json`.

**`database` field**: Leave empty to show all databases. Use a comma-separated list (e.g. `"db1,db2"`) to show specific databases.

## Data files

| File                               | Purpose                                                                                |
| ---------------------------------- | -------------------------------------------------------------------------------------- |
| `~/.config/dbx/config.json`        | UI preferences (theme, layout, status message duration)                                |
| `~/.cache/dbx/connections.json`    | Saved connections                                                                      |
| `~/.cache/dbx/history.json`        | Executed queries (filterable history popup, ctrl+p/n buffer browse)                    |
| `~/.cache/dbx/query-contents.json` | Full editor buffer text per `connection_id:database` (drafts; not the same as history) |
| `~/.cache/dbx/open-tabs.json`      | Ordered list of open query tabs (`connection_id:database` keys) for session restore    |
| `~/.cache/dbx/ai_sessions.json`    | AI chat messages and session ids per `connection_id:database`                          |
| `~/.cache/dbx/aiagentfolder` (default) | AI CLI subprocess cwd when `ai.disable_agent_workdir` is false and `agent_workdir` is empty; configurable |

## Layout

Each pane’s **top border** shows its name and focus key: `[e] Explorer`, `[q] Query Editor`, `[r] Results`, and **`[a] AI Assistant`** when the AI column is visible. The AI column is hidden by default; press **`a`** (global) to show it and focus, or open the palette (**space**) and press **`a`** to toggle visibility without changing focus first. The query editor border also shows the **active connection name** (or id) and **database**, e.g. `· Local PG / myapp`, or `—` when nothing is selected. Inside the query pane, the **top row** is a tab bar (when no tabs are open, it shows idle / no connection).

## Keybindings

### Global
| Key      | Action                                                                                                                                                                                               |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `e`      | Focus explorer                                                                                                                                                                                       |
| `q`      | Focus editor                                                                                                                                                                                         |
| `r`      | Focus results                                                                                                                                                                                        |
| `a`      | Show the AI pane if it was hidden, then focus it. If the AI pane already has focus, `a` does **not** hide it — open the palette (**space**) and press **`a`** to **toggle** the AI column on or off. |
| `space`  | Open command palette (then press a letter: e.g. **`n`** add connection with explorer focused, **`a`** toggle AI pane from explorer / editor / results / AI)                                          |
| `?`      | Toggle help (fixed-size popup; `j`/`k`, `g`/`G`, PgUp/PgDn scroll; `?`/`esc`/`q` close)                                                                                                              |
| `ctrl+c` | Quit                                                                                                                                                                                                 |

### Explorer
| Key           | Action                                                                                                                                                      |
| ------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `j` / `k`     | Navigate                                                                                                                                                    |
| `enter` / `l` | Expand/collapse (connections, databases, tables)                                                                                                            |
| `h`           | Collapse current branch (from child -> parent)                                                                                                              |
| `s`           | Append quick `SELECT * … LIMIT/TOP 100` at end of editor (blank line before it), run it; focus stays here (`r` for results)                                 |
| `v`           | Open popup with recreate DDL for the focused table or view (`y` copy, scroll keys, `esc` close; MySQL: `SHOW CREATE`, Postgres/SQLite/MSSQL: assembled DDL) |
| `f`           | Set filter for tables. Set it empty to see all tables                                                                                                       |
| `g` / `G`     | First / last item                                                                                                                                           |

### Editor (Normal mode)
| Key                 | Action                                                                                                                                                                                                            |
| ------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `tab`               | Next query tab (explorer selection follows)                                                                                                                                                                       |
| `shift+tab`         | Previous query tab                                                                                                                                                                                                |
| `i` / `o`           | Enter insert mode                                                                                                                                                                                                 |
| `enter`             | Execute query under cursor. If the block is **only** multiple `DELETE` and/or `UPDATE` statements separated by `;`, they run **one after another**; the results grid shows `#` and `rows_affected` per statement. |
| `u`                 | Undo last edit (per tab; up to 200 steps). One undo step covers a whole insert session (from `i`/`a`/… until `esc`), plus normal-mode edits                                                                       |
| `ctrl+r`            | Redo (normal mode only; see insert mode for run-query)                                                                                                                                                            |
| `ctrl+p` / `ctrl+n` | Browse history (replace buffer)                                                                                                                                                                                   |
| `backspace`         | Open **history popup** — filter and pick a query to append (see **History** below)                                                                                                                                |
| `dd`                | Delete line                                                                                                                                                                                                       |
| `dq`                | Delete query                                                                                                                                                                                                      |
| `dw`                | Delete current word                                                                                                                                                                                               |
| `d$`                | Delete to end of line                                                                                                                                                                                             |
| `d0`                | Delete to start of line                                                                                                                                                                                           |
| `yy`                | Yank/copy line                                                                                                                                                                                                    |
| `yq`                | Yank/copy query                                                                                                                                                                                                   |
| `yw`                | Yank/copy current word                                                                                                                                                                                            |
| `y$`                | Yank/copy to end of line                                                                                                                                                                                          |
| `y0`                | Yank/copy to start of line                                                                                                                                                                                        |
| `gg` / `G`          | Go to top / bottom                                                                                                                                                                                                |
| `J` / `K`           | Jump to next / previous query block (queries separated by blank lines)                                                                                                                                            |
| `h/j/k/l`           | Move cursor                                                                                                                                                                                                       |
| `w` / `b`           | Word forward / backward                                                                                                                                                                                           |

### Editor (Insert mode)
| Key                 | Action                                                                                                                                        |
| ------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `esc`               | Return to normal mode                                                                                                                         |
| `←` `→` `↑` `↓`     | Move cursor (arrows)                                                                                                                          |
| `tab`               | Open autocomplete from context (no need to type first) / accept selected suggestion; suggestions also appear as you type an identifier prefix |
| `ctrl+n` / `ctrl+p` | Next / previous suggestion (when autocomplete is open)                                                                                        |
| `ctrl+enter`        | Execute query                                                                                                                                 |
| `ctrl+r`            | Execute query                                                                                                                                 |

### Results
| Key             | Action                                                                                                                                                                                                                                                                                                                                                                                                         |
| --------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `j` / `k`       | Navigate rows                                                                                                                                                                                                                                                                                                                                                                                                  |
| `h` / `l`       | Navigate columns                                                                                                                                                                                                                                                                                                                                                                                               |
| `g` / `G`       | First / last row                                                                                                                                                                                                                                                                                                                                                                                               |
| `PgDn` / `PgUp` | Page down / up                                                                                                                                                                                                                                                                                                                                                                                                 |
| `C-d` / `C-u`   | Scroll down / up by half page                                                                                                                                                                                                                                                                                                                                                                                  |
| `s`             | Toggle **mark** on the current row (highlighted); use for multi-row delete/insert drafts                                                                                                                                                                                                                                                                                                                       |
| `S`             | Start **band select**: current row is marked; move with `j`/`k`/`g`/`G` to extend/shrink the marked range (all rows from anchor through cursor, inclusive)                                                                                                                                                                                                                                                     |
| `d`             | Append **DELETE** draft(s) to the query editor for all marked rows, or for the cursor row if none marked. Requires the last result from a **simple** `SELECT` of **one** base table (no `WITH`, `JOIN`, subquery in `FROM`). The `WHERE` clause uses the table’s **primary key columns** when they appear in the result; otherwise it falls back to matching **all** visible columns. Review before executing. |
| `i`             | Append **INSERT** draft(s) for the same row selection as `d`: one `INSERT INTO … (columns…) VALUES (…)` per row, with literals quoted for the connection’s driver. Same **simple** single-table `SELECT` requirement as `d`. Review before executing.                                                                                                                                                          |
| `u`             | **Update cell** — opens an input popup pre-filled with the current cell value. Enter a new value and press `Enter` to append an `UPDATE` statement to the query editor (`Esc` to cancel). Uses PK columns in the `WHERE` clause when possible. Multiple updates are appended without blank line separation so batch execution treats them together.                                                            |
| `esc`           | Clear marks / exit band mode                                                                                                                                                                                                                                                                                                                                                                                   |
| `v`             | **Full cell** popup — `y` copy; `f` pretty-print valid JSON (press again for raw); while JSON mode is on, each cell you move to with `h`/`l`/`j`/`k` is pretty-printed when valid; `h`/`l` or `←`/`→` adjacent column; `j`/`k` or `↑`/`↓` adjacent row; long lines wrap to the popup width; `PgDn`/`PgUp`/`C-d`/`C-u`,  and `g`/`G` scroll when content exceeds the viewport; `esc` close                      |

The **active cell** uses a stronger highlight than the rest of the cursor row.

### AI Assistant

This pane is **experimental**: behavior, CLI integration, and UX may change or break as it is tried out; it is not treated as a stable product surface yet.

Shown as a **right column** when visible; width is `layout.ai_pane_width_pct` in `config.json`. Chats are **per** `connection:database` (same key as query tabs). The bottom **status row** in the pane summarizes mode, scroll %, and optional history size warning.

By default, dbx sets the configured AI CLI’s **process working directory** to an isolated folder under the cache (see **`disable_agent_workdir`** / **`agent_workdir`** above), not the directory you launched dbx from. Many CLI AI apps keep on-disk session or index state relative to that directory. If you set **`disable_agent_workdir`** to **`true`**, the AI CLI inherits dbx’s cwd again; in that mode, to have the tool **resume** the same internal session that lines up with dbx’s persisted chat id in `~/.cache/dbx/ai_sessions.json`, run dbx from the **same working directory** whenever you use a given connection/database. If you switch launch folders with isolation disabled, the CLI may not honor the stored session id and can behave like a new session internally.

| Mode / keys          | Action                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Normal** (default) | Transcript area: **block cursor** (reversed cell) on the current line/column — move with `h`/`j`/`k`/`l` or arrows; page keys (`f`/`b`, PgUp/PgDn, `d`/`u`, space) and the **mouse wheel** scroll the view and keep the cursor oriented. Long lines scroll horizontally with the cursor.                                                                                                                                                                                                                                                                                                                         |
| `J` / `K`            | Jump to the **next** / **previous** fenced `sql` block (same idea as **J**/**K** between query blocks in the editor).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `i`                  | **Insert** — type in the prompt area; `enter` sends (when not loading); `alt+enter` inserts a newline in the prompt.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `esc`                | Insert → Normal (blur prompt).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `enter` (Normal)     | Copy the fenced `sql` block **on the cursor line** (or the nearest block) to the **query editor**.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `@` (Insert)         | Open table picker (schema tables; filter by typing).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `#` (Insert)         | Open column picker for the current database.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `/clear` (Insert)    | **Whole prompt line only** (case-insensitive): clear the transcript and start a new CLI chat session.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `/results` (Insert)  | **Line must start with** `/results` (case-insensitive), then optional text. Sends your question with the **results pane** context: the last **successful** query’s SQL plus the current grid as tab-separated rows (after any `@` DDL blocks). If you send **`/results` alone**, the default question is *Summarize this result set.* Requires a finished, successful query with stored SQL — if not (loading, error, or empty), dbx shows a status message and **does not** call the AI; your prompt text is restored. Size of the query+rows block is capped by **`max_results_context_kb`** in `config.json`. |

While the prompt field is active (Insert or an `@`/`#` menu), global `e`/`q`/`r`/`a` shortcuts are suppressed until you `esc` the overlay or leave Insert.

### Command Palette (space)

Press **space** to open the palette, then the **second** key (e.g. **space** then **a** toggles the AI pane from explorer, editor, results, or AI). **Add connection** is **`n`** (explorer palette only), not `a`.

| Panel    | Commands                                                                                                              |
| -------- | --------------------------------------------------------------------------------------------------------------------- |
| Explorer | `n` add connection, `e` edit, `d` delete, `R` refresh, `t` toggle explorer, `a` toggle AI pane, `f` fullscreen        |
| Editor   | `x` execute, `e` explain, `c` clear, `D` close tab (confirm), `t` toggle explorer, `a` toggle AI pane, `f` fullscreen |
| Results  | `y` copy cell, `Y` copy row, `e` export CSV, `j` export JSON, `t` toggle explorer, `a` toggle AI pane, `f` fullscreen |
| AI       | `t` toggle explorer, `a` toggle AI pane, `f` fullscreen                                                               |

## History

Query history is saved per connection/database to `~/.cache/dbx/history.json`. To use it:

1. **Select a database** in the explorer (expand a connection, then select a database). History is loaded when you select a database.
2. **Execute a query** — it is saved to history for that connection/database.
3. **Browse history** with `ctrl+p` (older) and `ctrl+n` (newer) in the editor — works in both Normal and Insert mode (replaces the buffer).
4. **History popup** (normal mode only): `backspace` opens a centered list of past queries.
   - **Filter**: type to narrow the list — substring match, case-insensitive. 
   - **Move**: `↑` / `↓` .
   - **Insert**: `enter` appends the selected query at the end of the editor buffer.
   - **Close**: `esc`, or `backspace` when the filter is empty (otherwise `backspace` deletes the last filter character).
   - **Delete from history**: `ctrl+d` opens a confirmation for the highlighted entry. 

## License

Dbx is licensed under the MIT License. See [LICENSE](LICENSE) for details.

## Thanks For Visiting

Hope you liked it. Wanna **[buy Me a coffee](https://www.buymeacoffee.com/nospor)**?

