package db

import (
	"context"
	"database/sql"
	"fmt"

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
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("sqlite open: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
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
