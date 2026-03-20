package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (d *mysqlDriver) TableDDL(ctx context.Context, database, table string, isView bool) (string, error) {
	dbName := d.dbForQuery(database)
	if dbName != "" {
		if _, err := d.db.ExecContext(ctx, "USE `"+strings.ReplaceAll(dbName, "`", "``")+"`"); err != nil {
			return "", err
		}
	}
	q := "SHOW CREATE TABLE `" + strings.ReplaceAll(table, "`", "``") + "`"
	if isView {
		q = "SHOW CREATE VIEW `" + strings.ReplaceAll(table, "`", "``") + "`"
	}
	var name, ddl sql.NullString
	var err error
	if isView {
		// SHOW CREATE VIEW: View, Create View, character_set_client, collation_connection
		row := d.db.QueryRowContext(ctx, q)
		var csc, cc sql.NullString
		err = row.Scan(&name, &ddl, &csc, &cc)
	} else {
		row := d.db.QueryRowContext(ctx, q)
		err = row.Scan(&name, &ddl)
	}
	if err != nil {
		return "", fmt.Errorf("mysql show create: %w", err)
	}
	if !ddl.Valid {
		return "", fmt.Errorf("empty DDL from server")
	}
	return ddl.String, nil
}
