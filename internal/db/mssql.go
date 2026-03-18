package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/microsoft/go-mssqldb"

	"github.com/robertn/dbx/internal/config"
)

type mssqlDriver struct {
	db   *sql.DB
	conn config.Connection
}

func (d *mssqlDriver) Connect(ctx context.Context, conn config.Connection) error {
	d.conn = conn
	dsn := buildMSSQLDSN(conn)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return fmt.Errorf("mssql connect: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("mssql ping: %w", err)
	}
	d.db = db
	return nil
}

func buildMSSQLDSN(conn config.Connection) string {
	host := conn.Host
	if host == "" {
		host = "localhost"
	}
	port := conn.Port
	if port == 0 {
		port = 1433
	}
	db := conn.Database
	if strings.Contains(db, ",") {
		db = strings.SplitN(db, ",", 2)[0]
	}
	return fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s",
		conn.User, conn.Password, host, port, db)
}

func (d *mssqlDriver) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *mssqlDriver) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

func (d *mssqlDriver) Databases(ctx context.Context) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT name FROM sys.databases WHERE state_desc = 'ONLINE' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

func (d *mssqlDriver) Tables(ctx context.Context, database string) ([]string, error) {
	db := d.dbForQuery(database)
	query := fmt.Sprintf("SELECT TABLE_NAME FROM [%s].INFORMATION_SCHEMA.TABLES WHERE TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME", db)
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

func (d *mssqlDriver) Views(ctx context.Context, database string) ([]string, error) {
	db := d.dbForQuery(database)
	query := fmt.Sprintf("SELECT TABLE_NAME FROM [%s].INFORMATION_SCHEMA.VIEWS ORDER BY TABLE_NAME", db)
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

func (d *mssqlDriver) Columns(ctx context.Context, database, table string) ([]ColumnInfo, error) {
	db := d.dbForQuery(database)
	query := fmt.Sprintf(
		"SELECT COLUMN_NAME, DATA_TYPE FROM [%s].INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = @p1 ORDER BY ORDINAL_POSITION",
		db)
	rows, err := d.db.QueryContext(ctx, query, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []ColumnInfo
	for rows.Next() {
		var c ColumnInfo
		if err := rows.Scan(&c.Name, &c.DataType); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func (d *mssqlDriver) Query(ctx context.Context, database, sqlStr string) (*QueryResult, error) {
	db := d.dbForQuery(database)
	if db != "" {
		if _, err := d.db.ExecContext(ctx, fmt.Sprintf("USE [%s]", db)); err != nil {
			return &QueryResult{Error: err.Error()}, nil
		}
	}
	rows, err := d.db.QueryContext(ctx, sqlStr)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	defer rows.Close()
	return scanSQLRows(rows)
}

func (d *mssqlDriver) Exec(ctx context.Context, database, sqlStr string) (*QueryResult, error) {
	db := d.dbForQuery(database)
	if db != "" {
		if _, err := d.db.ExecContext(ctx, fmt.Sprintf("USE [%s]", db)); err != nil {
			return &QueryResult{Error: err.Error()}, nil
		}
	}
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

func (d *mssqlDriver) dbForQuery(database string) string {
	if database != "" {
		return database
	}
	db := d.conn.Database
	if strings.Contains(db, ",") {
		db = strings.SplitN(db, ",", 2)[0]
	}
	return db
}
