# dbx — TUI Database Client

A terminal-based database client written in Go with vim-mode editing, multi-database support, and a command palette.

## Features

- **Databases**: PostgreSQL, MySQL, SQLite, MSSQL
- **Three-panel layout**: Explorer | Query Editor | Results
- **Vim mode**: Normal and Insert mode with motions (h/j/k/l, w/b, gg/G, dd, etc.)
- **Multi-query support**: Separate queries by blank lines; execute the one under the cursor
- **SQL syntax highlighting** via chroma
- **Tab autocomplete** for table names, column names, and SQL keywords
- **Query history** per connection/database (ctrl+p/n to browse)
- **Command palette** (space) with context-aware commands per panel
- **Fullscreen** any panel with space+f
- **Export** results to CSV or JSON
- **Copy** cell or row to clipboard
- **Themes**: terminal (default), dark, light — configurable in config file

## Install

```bash
go install github.com/robertn/dbx@latest
```

Or build from source:

```bash
git clone ...
cd dbx
go build -o dbx .
```

## Configuration

Config is stored in `~/.config/dbx/config.json`:

```json
{
  "connections": [
    {
      "id": "my-pg",
      "name": "My Postgres",
      "driver": "postgres",
      "host": "localhost",
      "port": 5432,
      "user": "postgres",
      "password": "secret",
      "database": "",
      "ssl_mode": "prefer"
    },
    {
      "id": "my-sqlite",
      "name": "Local DB",
      "driver": "sqlite",
      "file_path": "/path/to/database.db"
    }
  ],
  "layout": {
    "explorer_width_pct": 25,
    "editor_height_pct": 50
  },
  "theme": "dark"
}
```

**`database` field**: Leave empty to show all databases. Use a comma-separated list (e.g. `"db1,db2"`) to show specific databases.

## Keybindings

### Global
| Key      | Action               |
| -------- | -------------------- |
| `e`      | Focus explorer       |
| `q`      | Focus editor         |
| `r`      | Focus results        |
| `space`  | Open command palette |
| `?`      | Toggle help          |
| `ctrl+c` | Quit                 |

### Explorer
| Key           | Action                                |
| ------------- | ------------------------------------- |
| `j` / `k`     | Navigate                              |
| `enter` / `l` | Expand/collapse                       |
| `s`           | Quick `SELECT * FROM table LIMIT 100` |
| `g` / `G`     | First / last item                     |

### Editor (Normal mode)
| Key                 | Action                     |
| ------------------- | -------------------------- |
| `i` / `a` / `o`     | Enter insert mode          |
| `ctrl+enter`        | Execute query under cursor |
| `ctrl+p` / `ctrl+n` | Browse history             |
| `dd`                | Delete line                |
| `gg` / `G`          | Go to top / bottom         |
| `h/j/k/l`           | Move cursor                |
| `w` / `b`           | Word forward / backward    |

### Editor (Insert mode)
| Key          | Action                |
| ------------ | --------------------- |
| `esc`        | Return to normal mode |
| `tab`        | Trigger autocomplete  |
| `ctrl+enter` | Execute query         |

### Results
| Key       | Action           |
| --------- | ---------------- |
| `j` / `k` | Navigate rows    |
| `h` / `l` | Navigate columns |
| `g` / `G` | First / last row |

### Command Palette (space)
| Panel    | Commands                                                                     |
| -------- | ---------------------------------------------------------------------------- |
| Explorer | `a` add, `e` edit, `d` delete, `R` refresh, `t` toggle, `f` fullscreen       |
| Editor   | `x` execute, `c` clear, `f` fullscreen                                       |
| Results  | `y` copy cell, `Y` copy row, `e` export CSV, `j` export JSON, `f` fullscreen |

## History

Query history is saved per connection/database to `~/.config/dbx/history.json`. Browse with `ctrl+p` (older) and `ctrl+n` (newer) in the editor.
