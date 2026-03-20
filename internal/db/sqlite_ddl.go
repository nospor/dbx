package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (d *sqliteDriver) TableDDL(ctx context.Context, _, table string, isView bool) (string, error) {
	typ := "table"
	if isView {
		typ = "view"
	}
	var ddl sql.NullString
	row := d.db.QueryRowContext(ctx,
		"SELECT sql FROM sqlite_master WHERE type = ? AND name = ?",
		typ, table)
	if err := row.Scan(&ddl); err != nil {
		return "", fmt.Errorf("sqlite master: %w", err)
	}
	if !ddl.Valid || strings.TrimSpace(ddl.String) == "" {
		return "", fmt.Errorf("no %s definition for %q", typ, table)
	}
	var b strings.Builder
	b.WriteString(ddl.String)
	b.WriteString(";\n")
	if !isView {
		rows, err := d.db.QueryContext(ctx,
			"SELECT sql FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL ORDER BY name",
			table)
		if err != nil {
			return b.String(), nil // best effort
		}
		defer rows.Close()
		for rows.Next() {
			var idxSQL sql.NullString
			if err := rows.Scan(&idxSQL); err != nil {
				continue
			}
			if idxSQL.Valid && strings.TrimSpace(idxSQL.String) != "" {
				b.WriteString(idxSQL.String)
				b.WriteString(";\n")
			}
		}
	}
	return strings.TrimSpace(b.String()), nil
}
