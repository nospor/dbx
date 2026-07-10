package db

import (
	"context"
	"fmt"

	"github.com/robertn/dbx/internal/config"
)

// ColumnInfo describes a single column in a table or view.
type ColumnInfo struct {
	Name     string
	DataType string
}

// TableColumn is one column belonging to a named table or view (used for bulk schema / autocomplete).
type TableColumn struct {
	Table    string
	Name     string
	DataType string
}

// QueryResult holds the result of a query execution.
type QueryResult struct {
	Columns []string
	Rows    [][]string
	Error   string
}

// Driver is the interface all database backends must implement.
type Driver interface {
	// Connect establishes a connection using the provided config.
	Connect(ctx context.Context, conn config.Connection) error

	// Close releases all resources.
	Close() error

	// Ping checks that the connection is alive.
	Ping(ctx context.Context) error

	// Databases returns a list of available database names.
	Databases(ctx context.Context) ([]string, error)

	// Tables returns a list of table and view names in the given database.
	Tables(ctx context.Context, database string) ([]string, error)

	// Views returns a list of view names in the given database.
	Views(ctx context.Context, database string) ([]string, error)

	// Columns returns column info for the given table in the given database.
	Columns(ctx context.Context, database, table string) ([]ColumnInfo, error)

	// AllTableColumns returns columns for all tables and views in the database (for autocomplete).
	AllTableColumns(ctx context.Context, database string) ([]TableColumn, error)

	// PrimaryKeyColumns returns primary-key column names in order (composite keys supported).
	// database is the active catalog when applicable; schema/table are logical names from the object.
	PrimaryKeyColumns(ctx context.Context, database, schema, table string) ([]string, error)

	// TableDDL returns SQL to recreate a table (CREATE TABLE + indexes/constraints as applicable)
	// or a view (CREATE VIEW). Quality is driver-specific (e.g. MySQL uses SHOW CREATE).
	TableDDL(ctx context.Context, database, table string, isView bool) (string, error)

	// Query executes a SELECT-like statement and returns rows.
	Query(ctx context.Context, database, sql string) (*QueryResult, error)

	// Exec executes a non-SELECT statement (INSERT, UPDATE, DELETE, DDL).
	Exec(ctx context.Context, database, sql string) (*QueryResult, error)
}

// New creates a Driver for the given connection config.
func New(conn config.Connection) (Driver, error) {
	switch conn.Driver {
	case "postgres", "postgresql":
		return &postgresDriver{}, nil
	case "mysql":
		return &mysqlDriver{}, nil
	case "sqlite", "sqlite3":
		return &sqliteDriver{}, nil
	case "mssql", "sqlserver":
		return &mssqlDriver{}, nil
	case "mongodb":
		return &mongoDriver{}, nil
	case "orientdb":
		return &orientDriver{}, nil
	case "oracle":
		return &oracleDriver{}, nil
	case "elasticsearch":
		return &elasticDriver{}, nil
	default:
		return nil, fmt.Errorf("unsupported driver: %q", conn.Driver)
	}
}
