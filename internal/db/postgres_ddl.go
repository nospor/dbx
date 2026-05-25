package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const pgExplorerSchema = "public"

func (d *postgresDriver) TableDDL(ctx context.Context, database, table string, isView bool) (string, error) {
	pool, err := d.poolForDB(ctx, database)
	if err != nil {
		return "", err
	}
	defer func() {
		if pool != d.pool {
			pool.Close()
		}
	}()

	schema := pgExplorerSchema
	if isView {
		var def *string
		q := `SELECT pg_get_viewdef(format('%I.%I', $1::name, $2::name)::regclass, true)`
		if err := pool.QueryRow(ctx, q, schema, table).Scan(&def); err != nil {
			return "", fmt.Errorf("postgres view ddl: %w", err)
		}
		if def == nil || strings.TrimSpace(*def) == "" {
			return "", fmt.Errorf("empty view definition")
		}
		qSchema := pgx.Identifier{schema}.Sanitize()
		qTbl := pgx.Identifier{table}.Sanitize()
		return fmt.Sprintf("CREATE OR REPLACE VIEW %s.%s AS\n%s;", qSchema, qTbl, strings.TrimSpace(*def)), nil
	}

	// Table: columns
	colQ := `
SELECT a.attname,
       pg_catalog.format_type(a.atttypid, a.atttypmod),
       a.attnotnull,
       pg_catalog.pg_get_expr(ad.adbin, ad.adrelid)
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON a.attrelid = c.oid
JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid
LEFT JOIN pg_catalog.pg_attrdef ad ON a.attrelid = ad.adrelid AND a.attnum = ad.adnum
WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`
	rows, err := pool.Query(ctx, colQ, schema, table)
	if err != nil {
		return "", fmt.Errorf("postgres columns: %w", err)
	}
	defer rows.Close()

	type col struct {
		name    string
		typ     string
		notNull bool
		defExpr *string
	}
	var cols []col
	for rows.Next() {
		var c col
		if err := rows.Scan(&c.name, &c.typ, &c.notNull, &c.defExpr); err != nil {
			return "", err
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(cols) == 0 {
		return "", fmt.Errorf("table %q not found or has no columns", table)
	}

	pk, err := d.PrimaryKeyColumns(ctx, database, schema, table)
	if err != nil {
		pk = nil
	}
	pkSet := make(map[string]struct{}, len(pk))
	for _, c := range pk {
		pkSet[strings.ToLower(c)] = struct{}{}
	}

	qSchema := pgx.Identifier{schema}.Sanitize()
	qTbl := pgx.Identifier{table}.Sanitize()

	var colLines []string
	for _, c := range cols {
		qc := pgx.Identifier{c.name}.Sanitize()
		line := fmt.Sprintf("    %s %s", qc, c.typ)
		_, isPK := pkSet[strings.ToLower(c.name)]
		if c.notNull && !isPK {
			line += " NOT NULL"
		}
		if c.defExpr != nil && strings.TrimSpace(*c.defExpr) != "" {
			line += " DEFAULT " + *c.defExpr
		}
		colLines = append(colLines, line)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("CREATE TABLE %s.%s (\n", qSchema, qTbl))
	b.WriteString(strings.Join(colLines, ",\n"))
	if len(pk) > 0 {
		pquoted := make([]string, len(pk))
		for i, c := range pk {
			pquoted[i] = pgx.Identifier{c}.Sanitize()
		}
		b.WriteString(",\n    PRIMARY KEY (")
		b.WriteString(strings.Join(pquoted, ", "))
		b.WriteString(")")
	}
	b.WriteString("\n);\n")

	// Secondary indexes (indexdef is a full CREATE [UNIQUE] INDEX statement)
	idxRows, err := pool.Query(ctx, `
SELECT indexname, indexdef
FROM pg_indexes
WHERE schemaname = $1 AND tablename = $2
ORDER BY indexname`,
		schema, table)
	if err == nil {
		var pkIdxName string
		if err := pool.QueryRow(ctx, `
SELECT ri.relname
FROM pg_index i
JOIN pg_class ti ON ti.oid = i.indrelid
JOIN pg_class ri ON ri.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = ti.relnamespace
WHERE n.nspname = $1 AND ti.relname = $2 AND i.indisprimary`,
			schema, table).Scan(&pkIdxName); err != nil {
			pkIdxName = ""
		}

		for idxRows.Next() {
			var iname, idef string
			if err := idxRows.Scan(&iname, &idef); err != nil {
				continue
			}
			if pkIdxName != "" && iname == pkIdxName {
				continue
			}
			st := strings.TrimSpace(idef)
			if st != "" {
				if !strings.HasSuffix(st, ";") {
					st += ";"
				}
				b.WriteString(st)
				b.WriteString("\n")
			}
		}
		idxRows.Close()
	}

	fkRows, err := pool.Query(ctx, `
SELECT con.conname, pg_catalog.pg_get_constraintdef(con.oid, true)
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class rel ON rel.oid = con.conrelid
JOIN pg_catalog.pg_namespace nsp ON nsp.oid = rel.relnamespace
WHERE nsp.nspname = $1 AND rel.relname = $2 AND con.contype = 'f'
ORDER BY con.conname`,
		schema, table)
	if err == nil {
		for fkRows.Next() {
			var cname, cdef string
			if err := fkRows.Scan(&cname, &cdef); err != nil {
				continue
			}
			cn := pgx.Identifier{cname}.Sanitize()
			b.WriteString(fmt.Sprintf("ALTER TABLE ONLY %s.%s ADD CONSTRAINT %s %s;\n", qSchema, qTbl, cn, cdef))
		}
		fkRows.Close()
	}

	return strings.TrimSpace(b.String()), nil
}
