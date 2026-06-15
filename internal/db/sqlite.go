package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/robertn/dbx/internal/config"
)

type sqliteDriver struct {
	db   *sql.DB
	conn config.Connection
}

func (d *sqliteDriver) Connect(ctx context.Context, conn config.Connection) error {
	d.conn = conn
	path := conn.FilePath
	if path == "" {
		path = conn.Database
	}
	if path == "" {
		return fmt.Errorf("sqlite: file path is required (set file_path or database in config)")
	}
	resolvedPath, err := resolveSQLitePath(path)
	if err != nil {
		return fmt.Errorf("sqlite path resolve: %w", err)
	}

	if !isSQLiteMemory(resolvedPath) {
		filePath := resolvedPath
		if idx := strings.Index(resolvedPath, "?"); idx != -1 {
			filePath = resolvedPath[:idx]
		}
		filePath = strings.TrimPrefix(filePath, "file://")
		filePath = strings.TrimPrefix(filePath, "file:")

		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			return fmt.Errorf("sqlite: database file %q does not exist", filePath)
		}
	}

	db, err := sql.Open("sqlite", resolvedPath)
	if err != nil {
		return fmt.Errorf("sqlite open: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		errMsg := err.Error()
		if strings.Contains(errMsg, "unable to open database file") {
			return fmt.Errorf("sqlite ping: %w (verify file permissions and that the path is valid)", err)
		}
		return fmt.Errorf("sqlite ping: %w", err)
	}
	d.db = db
	return nil
}

func (d *sqliteDriver) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *sqliteDriver) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// SQLite has no concept of multiple databases in the same connection.
func (d *sqliteDriver) Databases(ctx context.Context) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dbs []string
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return nil, err
		}
		dbs = append(dbs, name)
	}
	return dbs, rows.Err()
}

func (d *sqliteDriver) Tables(ctx context.Context, _ string) ([]string, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

func (d *sqliteDriver) Views(ctx context.Context, _ string) ([]string, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='view' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

func (d *sqliteDriver) Columns(ctx context.Context, _, table string) ([]ColumnInfo, error) {
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []ColumnInfo
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, ColumnInfo{Name: name, DataType: typ})
	}
	return cols, rows.Err()
}

func (d *sqliteDriver) AllTableColumns(ctx context.Context, _ string) ([]TableColumn, error) {
	// SQLite 3.33+ supports table-valued pragma; fall back per-table if unavailable.
	rows, err := d.db.QueryContext(ctx, `
		SELECT m.name, p.name, p.type
		FROM sqlite_master AS m
		JOIN pragma_table_info(m.name) AS p
		WHERE m.type IN ('table','view')
		  AND m.name NOT GLOB 'sqlite_*'
		ORDER BY m.name, p.cid`)
	if err == nil {
		defer rows.Close()
		var out []TableColumn
		for rows.Next() {
			var tc TableColumn
			if err := rows.Scan(&tc.Table, &tc.Name, &tc.DataType); err != nil {
				return nil, err
			}
			out = append(out, tc)
		}
		return out, rows.Err()
	}

	tables, err := d.Tables(ctx, "")
	if err != nil {
		return nil, err
	}
	views, _ := d.Views(ctx, "")
	names := make([]string, 0, len(tables)+len(views))
	names = append(names, tables...)
	names = append(names, views...)
	var out []TableColumn
	for _, name := range names {
		cols, err := d.Columns(ctx, "", name)
		if err != nil {
			continue
		}
		for _, c := range cols {
			out = append(out, TableColumn{Table: name, Name: c.Name, DataType: c.DataType})
		}
	}
	return out, nil
}

func (d *sqliteDriver) PrimaryKeyColumns(ctx context.Context, _, _, table string) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type pkcol struct {
		ord int
		nam string
	}
	var pks []pkcol
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		if pk > 0 {
			pks = append(pks, pkcol{ord: pk, nam: name})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(pks, func(i, j int) bool { return pks[i].ord < pks[j].ord })
	out := make([]string, 0, len(pks))
	for _, p := range pks {
		out = append(out, p.nam)
	}
	return out, nil
}

func (d *sqliteDriver) Query(ctx context.Context, _ string, sqlStr string) (*QueryResult, error) {
	rows, err := d.db.QueryContext(ctx, sqlStr)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	defer rows.Close()
	return scanSQLRows(rows)
}

func (d *sqliteDriver) Exec(ctx context.Context, _ string, sqlStr string) (*QueryResult, error) {
	result, err := d.db.ExecContext(ctx, sqlStr)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	affected, _ := result.RowsAffected()
	return &QueryResult{
		Columns: []string{"rows_affected"},
		Rows:    [][]string{{fmt.Sprintf("%d", affected)}},
	}, nil
}

func isSQLiteMemory(path string) bool {
	return path == ":memory:" ||
		strings.Contains(path, "mode=memory") ||
		strings.Contains(path, ":memory:")
}

func expandTilde(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	} else if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	} else if strings.HasPrefix(path, "~\\") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func resolveSQLitePath(dsn string) (string, error) {
	if isSQLiteMemory(dsn) {
		return dsn, nil
	}

	// Separate query parameters if present
	basePath := dsn
	params := ""
	if idx := strings.Index(dsn, "?"); idx != -1 {
		basePath = dsn[:idx]
		params = dsn[idx:]
	}

	hasFilePrefix := false
	filePrefix := ""
	if strings.HasPrefix(basePath, "file://") {
		hasFilePrefix = true
		filePrefix = "file://"
		basePath = strings.TrimPrefix(basePath, "file://")
	} else if strings.HasPrefix(basePath, "file:") {
		hasFilePrefix = true
		filePrefix = "file:"
		basePath = strings.TrimPrefix(basePath, "file:")
	}

	basePath = expandTilde(basePath)

	dir := filepath.Dir(basePath)
	if dir != "." && dir != "/" && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create sqlite parent directory %q: %w", dir, err)
		}
	}

	resolved := basePath
	if hasFilePrefix {
		resolved = filePrefix + resolved
	}
	resolved = resolved + params
	return resolved, nil
}
