package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	go_ora "github.com/sijms/go-ora/v2"

	"github.com/robertn/dbx/internal/config"
)

type oracleDriver struct {
	db   *sql.DB
	conn config.Connection
}

func (d *oracleDriver) Connect(ctx context.Context, conn config.Connection) error {
	d.conn = conn
	
	// First try connecting with Database as Service Name
	dsn := buildOracleDSN(conn, false)
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return fmt.Errorf("oracle connect: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		// If ORA-12514, the listener doesn't know the service. It might be a SID.
		if strings.Contains(err.Error(), "ORA-12514") {
			dsnSID := buildOracleDSN(conn, true)
			dbSID, errSID := sql.Open("oracle", dsnSID)
			if errSID == nil {
				if errPing := dbSID.PingContext(ctx); errPing == nil {
					d.db = dbSID
					return nil
				}
				dbSID.Close()
			}
		}
		return fmt.Errorf("oracle ping: %w", err)
	}
	d.db = db
	return nil
}

func buildOracleDSN(conn config.Connection, asSID bool) string {
	host := conn.Host
	if host == "" {
		host = "localhost"
	}
	port := conn.Port
	if port == 0 {
		port = 1521
	}
	db := conn.Database
	if db == "" {
		db = "ORCLCDB"
	}
	
	options := map[string]string{}
	service := db
	if asSID {
		service = ""
		options["SID"] = db
	}
	
	return go_ora.BuildUrl(host, port, service, conn.User, conn.Password, options)
}

func (d *oracleDriver) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *oracleDriver) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

func (d *oracleDriver) Databases(ctx context.Context) ([]string, error) {
	// In Oracle, schemas (users) are typically treated as databases in generic tools.
	rows, err := d.db.QueryContext(ctx, "SELECT username FROM all_users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

func (d *oracleDriver) Tables(ctx context.Context, database string) ([]string, error) {
	schema := d.schemaForQuery(database)
	rows, err := d.db.QueryContext(ctx,
		"SELECT table_name FROM all_tables WHERE owner = :1 ORDER BY table_name",
		schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

func (d *oracleDriver) Views(ctx context.Context, database string) ([]string, error) {
	schema := d.schemaForQuery(database)
	rows, err := d.db.QueryContext(ctx,
		"SELECT view_name FROM all_views WHERE owner = :1 ORDER BY view_name",
		schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

func (d *oracleDriver) Columns(ctx context.Context, database, table string) ([]ColumnInfo, error) {
	schema := d.schemaForQuery(database)
	rows, err := d.db.QueryContext(ctx,
		"SELECT column_name, data_type FROM all_tab_columns WHERE owner = :1 AND table_name = :2 ORDER BY column_id",
		schema, table)
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

func (d *oracleDriver) AllTableColumns(ctx context.Context, database string) ([]TableColumn, error) {
	schema := d.schemaForQuery(database)
	rows, err := d.db.QueryContext(ctx,
		`SELECT table_name, column_name, data_type
		 FROM all_tab_columns
		 WHERE owner = :1
		 ORDER BY table_name, column_id`,
		schema)
	if err != nil {
		return nil, err
	}
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

func (d *oracleDriver) PrimaryKeyColumns(ctx context.Context, database, schema, table string) ([]string, error) {
	sch := schema
	if sch == "" {
		sch = d.schemaForQuery(database)
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT cols.column_name
		 FROM all_constraints cons, all_cons_columns cols
		 WHERE cols.table_name = :1
		   AND cons.constraint_type = 'P'
		   AND cons.constraint_name = cols.constraint_name
		   AND cons.owner = cols.owner
		   AND cons.owner = :2
		 ORDER BY cols.position`,
		table, sch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

func (d *oracleDriver) TableDDL(ctx context.Context, database, table string, isView bool) (string, error) {
	// Basic implementation for Oracle TableDDL since retrieving exact DDL usually requires DBMS_METADATA
	schema := d.schemaForQuery(database)
	var ddl string
	if isView {
		err := d.db.QueryRowContext(ctx, "SELECT text FROM all_views WHERE owner = :1 AND view_name = :2", schema, table).Scan(&ddl)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("CREATE OR REPLACE VIEW %s AS\n%s", table, ddl), nil
	}

	// For tables, we'll try DBMS_METADATA.GET_DDL
	err := d.db.QueryRowContext(ctx, "SELECT DBMS_METADATA.GET_DDL('TABLE', :1, :2) FROM DUAL", table, schema).Scan(&ddl)
	if err != nil {
		return "", fmt.Errorf("could not get table DDL (requires DBMS_METADATA privileges): %w", err)
	}
	return ddl, nil
}

func (d *oracleDriver) Query(ctx context.Context, database, sqlStr string) (*QueryResult, error) {
	// Oracle doesn't really 'USE schema', we just execute the query.
	// The user should qualify tables with the schema if needed, or rely on the connected user.
	rows, err := d.db.QueryContext(ctx, sqlStr)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	defer rows.Close()
	return scanSQLRows(rows)
}

func (d *oracleDriver) Exec(ctx context.Context, database, sqlStr string) (*QueryResult, error) {
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

func (d *oracleDriver) schemaForQuery(database string) string {
	if database != "" {
		return database
	}
	// Fallback to connected user if no database (schema) selected
	return strings.ToUpper(d.conn.User)
}
