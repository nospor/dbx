# Dbx — TUI Database Client

A terminal-based database client written in Go with vim-mode editing, multi-database support, and a command palette.

## Features

- **Databases**: PostgreSQL, MySQL, SQLite, MSSQL
- **Three-panel layout**: Explorer | Query Editor | Results — each pane shows **key hints** on the bottom border (long lines truncate if the pane is narrow)
- **Vim mode**: Normal and Insert mode with motions (h/j/k/l, w/b, gg/G, dd, etc.)
- **Multi-query support**: Separate queries by blank lines; execute the one under the cursor
- **SQL syntax highlighting** via chroma
- **Tab autocomplete** for table names, column names, and SQL keywords (columns are loaded automatically when you expand a database / refresh schema—no need to open each table first)
- **Query history** per connection/database (ctrl+p/n to browse) — stored in `history.json`
- **Per-database editor drafts** — the query pane is remembered per connection/database; drafts save when you leave Insert mode (`esc`) and when switching databases; stored in `query-contents.json` (separate from history)
- **Query tabs** — each connection/database opens as a tab in the query editor (`tab` / `shift+tab` to cycle; command palette `D` opens a **centered confirm popup** — `y` or `enter` to close, `n` / `esc` / `q` to cancel). Open tabs are restored on startup (`open-tabs.json`)
- **Command palette** (space) with context-aware commands per panel
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
    "editor_height_pct": 50
  },
  "theme": "catppuccin-mocha",
  "status_message_seconds": 5
}
```

Available `theme` values: `terminal`, `dark`, `light`, `catppuccin-mocha`, `catppuccin-latte`, `nord`, `gruvbox-dark`.

Connections are stored separately in `~/.cache/dbx/connections.json`.

**`database` field**: Leave empty to show all databases. Use a comma-separated list (e.g. `"db1,db2"`) to show specific databases.

## Data files

| File                               | Purpose                                                                                |
| ---------------------------------- | -------------------------------------------------------------------------------------- |
| `~/.config/dbx/config.json`        | UI preferences (theme, layout, status message duration)                                |
| `~/.cache/dbx/connections.json`    | Saved connections                                                                      |
| `~/.cache/dbx/history.json`        | Executed queries (history popup / ctrl+p n)                                            |
| `~/.cache/dbx/query-contents.json` | Full editor buffer text per `connection_id:database` (drafts; not the same as history) |
| `~/.cache/dbx/open-tabs.json`      | Ordered list of open query tabs (`connection_id:database` keys) for session restore    |

## Layout

Each pane’s **top border** shows its name and focus key: `[e] Explorer`, `[q] Query Editor`, `[r] Results`. The query editor border also shows the **active connection name** (or id) and **database**, e.g. `· Local PG / myapp`, or `—` when nothing is selected. Inside the query pane, the **top row** is a tab bar (when no tabs are open, it shows idle / no connection).

## Keybindings

### Global
| Key      | Action                                                                                          |
| -------- | ----------------------------------------------------------------------------------------------- |
| `e`      | Focus explorer                                                                                  |
| `q`      | Focus editor                                                                                    |
| `r`      | Focus results                                                                                   |
| `space`  | Open command palette                                                                            |
| `?`      | Toggle help (fixed-size popup; `j`/`k`, `g`/`G`, PgUp/PgDn scroll; `enter`/`?`/`esc`/`q` close) |
| `ctrl+c` | Quit                                                                                            |

### Explorer
| Key           | Action                                                                                                                                                      |
| ------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `j` / `k`     | Navigate                                                                                                                                                    |
| `enter` / `l` | Expand/collapse (connections, databases, tables)                                                                                                            |
| `h`           | Collapse current branch (from child -> parent)                                                                                                              |
| `s`           | Append quick `SELECT * … LIMIT/TOP 100` at end of editor (blank line before it), run it; focus stays here (`r` for results)                                 |
| `v`           | Open popup with recreate DDL for the focused table or view (`y` copy, scroll keys, `esc` close; MySQL: `SHOW CREATE`, Postgres/SQLite/MSSQL: assembled DDL) |
| `g` / `G`     | First / last item                                                                                                                                           |

### Editor (Normal mode)
| Key                 | Action                                                                                                                                                                                                            |
| ------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `tab`               | Next query tab (explorer selection follows)                                                                                                                                                                       |
| `shift+tab`         | Previous query tab                                                                                                                                                                                                |
| `i` / `a` / `o`     | Enter insert mode                                                                                                                                                                                                 |
| `enter`             | Execute query under cursor. If the block is **only** multiple `DELETE` and/or `UPDATE` statements separated by `;`, they run **one after another**; the results grid shows `#` and `rows_affected` per statement. |
| `u`                 | Undo last edit (per tab; up to 200 steps). One undo step covers a whole insert session (from `i`/`a`/… until `esc`), plus normal-mode edits                                                                       |
| `ctrl+r`            | Redo (normal mode only; see insert mode for run-query)                                                                                                                                                            |
| `ctrl+p` / `ctrl+n` | Browse history (replace buffer)                                                                                                                                                                                   |
| `backspace`         | History popup: pick a query to append                                                                                                                                                                             |
| `dd`                | Delete line                                                                                                                                                                                                       |
| `gg` / `G`          | Go to top / bottom                                                                                                                                                                                                |
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
| Key       | Action                                                                                                                                                                                                                                                                                                                                                                                                         |
| --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `j` / `k` | Navigate rows                                                                                                                                                                                                                                                                                                                                                                                                  |
| `h` / `l` | Navigate columns                                                                                                                                                                                                                                                                                                                                                                                               |
| `g` / `G` | First / last row                                                                                                                                                                                                                                                                                                                                                                                               |
| `s`       | Toggle **mark** on the current row (highlighted); use for multi-row actions                                                                                                                                                                                                                                                                                                                                    |
| `S`       | Start **band select**: current row is marked; move with `j`/`k`/`g`/`G` to extend/shrink the marked range (all rows from anchor through cursor, inclusive)                                                                                                                                                                                                                                                     |
| `d`       | Append **DELETE** draft(s) to the query editor for all marked rows, or for the cursor row if none marked. Requires the last result from a **simple** `SELECT` of **one** base table (no `WITH`, `JOIN`, subquery in `FROM`). The `WHERE` clause uses the table’s **primary key columns** when they appear in the result; otherwise it falls back to matching **all** visible columns. Review before executing. |
| `u`       | **Update cell** — opens an input popup pre-filled with the current cell value. Enter a new value and press `Enter` to append an `UPDATE` statement to the query editor (`Esc` to cancel). Uses PK columns in the `WHERE` clause when possible. Multiple updates are appended without blank line separation so batch execution treats them together.                                                            |
| `esc`     | Clear marks / exit band mode                                                                                                                                                                                                                                                                                                                                                                                   |
| `v`       | **Full cell** popup — `y` copy; `h`/`l` or `←`/`→` adjacent column; `j`/`k` or `↑`/`↓` adjacent row; `PgDn`/`PgUp`, `ctrl+f`/`ctrl+b`, and `g`/`G` scroll long cell text; `esc` close                                                                                                                                                                                                                          |

The **active cell** uses a stronger highlight than the rest of the cursor row.

### Command Palette (space)
| Panel    | Commands                                                                                          |
| -------- | ------------------------------------------------------------------------------------------------- |
| Explorer | `a` add, `e` edit, `d` delete, `R` refresh, `t` toggle, `f` fullscreen                            |
| Editor   | `x` execute, `c` clear, `D` close tab (confirm), `t` toggle explorer, `f` fullscreen              |
| Results  | `y` copy cell, `Y` copy row, `e` export CSV, `j` export JSON, `t` toggle explorer, `f` fullscreen |

## History

Query history is saved per connection/database to `~/.cache/dbx/history.json`. To use it:

1. **Select a database** in the explorer (expand a connection, then select a database). History is loaded when you select a database.
2. **Execute a query** — it is saved to history for that connection/database.
3. **Browse history** with `ctrl+p` (older) and `ctrl+n` (newer) in the editor — works in both Normal and Insert mode (replaces the buffer).
4. **History popup** (normal mode): `backspace` opens a list; `j`/`k` or `ctrl+p`/`ctrl+n` to move; `enter` appends the chosen query at the end of the editor; `d` opens an in-popup confirmation showing the exact query; `y` deletes it from history (`n`, `esc`, or `d` again cancels).

## License

Dbx is licensed under the MIT License. See [LICENSE](LICENSE) for details.

## Thanks For Visiting

Hope you liked it. Wanna **[buy Me a coffee](https://www.buymeacoffee.com/nospor)**?

