package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/robertn/dbx/internal/config"
)

type mysqlDriver struct {
	db   *sql.DB
	conn config.Connection
}

func (d *mysqlDriver) Connect(ctx context.Context, conn config.Connection) error {
	d.conn = conn
	dsn := buildMySQLDSN(conn)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("mysql connect: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("mysql ping: %w", err)
	}
	d.db = db
	return nil
}

func buildMySQLDSN(conn config.Connection) string {
	host := conn.Host
	if host == "" {
		host = "localhost"
	}
	port := conn.Port
	if port == 0 {
		port = 3306
	}
	db := conn.Database
	if strings.Contains(db, ",") {
		db = strings.SplitN(db, ",", 2)[0]
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&multiStatements=false",
		conn.User, conn.Password, host, port, db)
}

func (d *mysqlDriver) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *mysqlDriver) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

func (d *mysqlDriver) Databases(ctx context.Context) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

func (d *mysqlDriver) Tables(ctx context.Context, database string) ([]string, error) {
	db := d.dbForQuery(database)
	rows, err := d.db.QueryContext(ctx,
		"SELECT table_name FROM information_schema.tables WHERE table_schema = ? AND table_type = 'BASE TABLE' ORDER BY table_name",
		db)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

func (d *mysqlDriver) Views(ctx context.Context, database string) ([]string, error) {
	db := d.dbForQuery(database)
	rows, err := d.db.QueryContext(ctx,
		"SELECT table_name FROM information_schema.views WHERE table_schema = ? ORDER BY table_name",
		db)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

func (d *mysqlDriver) Columns(ctx context.Context, database, table string) ([]ColumnInfo, error) {
	db := d.dbForQuery(database)
	rows, err := d.db.QueryContext(ctx,
		"SELECT column_name, data_type FROM information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position",
		db, table)
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

func (d *mysqlDriver) Query(ctx context.Context, database, sqlStr string) (*QueryResult, error) {
	if database != "" {
		if _, err := d.db.ExecContext(ctx, "USE `"+database+"`"); err != nil {
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

func (d *mysqlDriver) Exec(ctx context.Context, database, sqlStr string) (*QueryResult, error) {
	if database != "" {
		if _, err := d.db.ExecContext(ctx, "USE `"+database+"`"); err != nil {
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

func (d *mysqlDriver) dbForQuery(database string) string {
	if database != "" {
		return database
	}
	db := d.conn.Database
	if strings.Contains(db, ",") {
		db = strings.SplitN(db, ",", 2)[0]
	}
	return db
}
