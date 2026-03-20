package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/robertn/dbx/internal/config"
)

type postgresDriver struct {
	pool *pgxpool.Pool
	conn config.Connection
}

func (d *postgresDriver) Connect(ctx context.Context, conn config.Connection) error {
	d.conn = conn
	dsn := buildPostgresDSN(conn)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("postgres connect: %w", err)
	}
	d.pool = pool
	return nil
}

func buildPostgresDSN(conn config.Connection) string {
	host := conn.Host
	if host == "" {
		host = "localhost"
	}
	port := conn.Port
	if port == 0 {
		port = 5432
	}
	db := conn.Database
	if strings.Contains(db, ",") {
		// Multiple databases — connect to first one for initial connection
		db = strings.SplitN(db, ",", 2)[0]
	}
	if db == "" {
		db = "postgres"
	}
	sslMode := conn.SSLMode
	if sslMode == "" {
		sslMode = "prefer"
	}
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, conn.User, conn.Password, db, sslMode)
}

func (d *postgresDriver) Close() error {
	if d.pool != nil {
		d.pool.Close()
	}
	return nil
}

func (d *postgresDriver) Ping(ctx context.Context) error {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	return conn.Ping(ctx)
}

func (d *postgresDriver) Databases(ctx context.Context) ([]string, error) {
	rows, err := d.pool.Query(ctx, "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		dbs = append(dbs, name)
	}
	return dbs, rows.Err()
}

func (d *postgresDriver) Tables(ctx context.Context, database string) ([]string, error) {
	pool, err := d.poolForDB(ctx, database)
	if err != nil {
		return nil, err
	}
	defer func() {
		if pool != d.pool {
			pool.Close()
		}
	}()

	rows, err := pool.Query(ctx,
		`SELECT table_name FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
		 ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

func (d *postgresDriver) Views(ctx context.Context, database string) ([]string, error) {
	pool, err := d.poolForDB(ctx, database)
	if err != nil {
		return nil, err
	}
	defer func() {
		if pool != d.pool {
			pool.Close()
		}
	}()

	rows, err := pool.Query(ctx,
		`SELECT table_name FROM information_schema.views
		 WHERE table_schema = 'public'
		 ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var views []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		views = append(views, name)
	}
	return views, rows.Err()
}

func (d *postgresDriver) Columns(ctx context.Context, database, table string) ([]ColumnInfo, error) {
	pool, err := d.poolForDB(ctx, database)
	if err != nil {
		return nil, err
	}
	defer func() {
		if pool != d.pool {
			pool.Close()
		}
	}()

	rows, err := pool.Query(ctx,
		`SELECT column_name, data_type FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = $1
		 ORDER BY ordinal_position`, table)
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

func (d *postgresDriver) Query(ctx context.Context, database, sql string) (*QueryResult, error) {
	pool, err := d.poolForDB(ctx, database)
	if err != nil {
		return nil, err
	}
	defer func() {
		if pool != d.pool {
			pool.Close()
		}
	}()

	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	defer rows.Close()
	return scanRows(rows)
}

func (d *postgresDriver) Exec(ctx context.Context, database, sql string) (*QueryResult, error) {
	pool, err := d.poolForDB(ctx, database)
	if err != nil {
		return nil, err
	}
	defer func() {
		if pool != d.pool {
			pool.Close()
		}
	}()

	tag, err := pool.Exec(ctx, sql)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	return &QueryResult{
		Columns: []string{"rows_affected"},
		Rows:    [][]string{{fmt.Sprintf("%d", tag.RowsAffected())}},
	}, nil
}

// poolForDB returns a connection pool targeting the specified database.
// If database matches the current connection, returns d.pool.
func (d *postgresDriver) poolForDB(ctx context.Context, database string) (*pgxpool.Pool, error) {
	if database == "" {
		return d.pool, nil
	}
	// Check if current pool is already connected to this database
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	var currentDB string
	err = conn.QueryRow(ctx, "SELECT current_database()").Scan(&currentDB)
	conn.Release()
	if err != nil {
		return nil, err
	}
	if currentDB == database {
		return d.pool, nil
	}

	// Create a new pool for the target database
	altConn := d.conn
	altConn.Database = database
	dsn := buildPostgresDSN(altConn)
	return pgxpool.New(ctx, dsn)
}

func scanRows(rows pgx.Rows) (*QueryResult, error) {
	fields := rows.FieldDescriptions()
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = string(f.Name)
	}

	var resultRows [][]string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			row[i] = formatSQLValue(v)
		}
		resultRows = append(resultRows, row)
	}
	if err := rows.Err(); err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	return &QueryResult{Columns: cols, Rows: resultRows}, nil
}
