package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

func (d *mssqlDriver) TableDDL(ctx context.Context, database, table string, isView bool) (string, error) {
	db := d.dbForQuery(database)
	if db != "" {
		if _, err := d.db.ExecContext(ctx, fmt.Sprintf("USE [%s]", strings.ReplaceAll(db, "]", "]]"))); err != nil {
			return "", err
		}
	}
	schema := "dbo"
	if isView {
		var def sql.NullString
		q := `
SELECT m.definition
FROM sys.sql_modules m
INNER JOIN sys.views v ON m.object_id = v.object_id
INNER JOIN sys.schemas s ON v.schema_id = s.schema_id
WHERE s.name = @p1 AND v.name = @p2`
		if err := d.db.QueryRowContext(ctx, q, schema, table).Scan(&def); err != nil {
			return "", fmt.Errorf("mssql view definition: %w", err)
		}
		if !def.Valid {
			return "", fmt.Errorf("empty view definition")
		}
		return fmt.Sprintf("CREATE VIEW %s AS\n%s", mssqlQualName(db, schema, table), strings.TrimSpace(def.String)), nil
	}

	rows, err := d.db.QueryContext(ctx, `
SELECT COLUMN_NAME, DATA_TYPE, CHARACTER_MAXIMUM_LENGTH, NUMERIC_PRECISION, NUMERIC_SCALE,
       IS_NULLABLE, COLUMN_DEFAULT
FROM INFORMATION_SCHEMA.COLUMNS
WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2
ORDER BY ORDINAL_POSITION`, schema, table)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type col struct {
		name string
		typ  string
		null bool
		def  sql.NullString
	}
	var cols []col
	for rows.Next() {
		var name, dataType, nullable string
		var charLen sql.NullInt64
		var numPrec, numScale sql.NullInt64
		var colDef sql.NullString
		if err := rows.Scan(&name, &dataType, &charLen, &numPrec, &numScale, &nullable, &colDef); err != nil {
			return "", err
		}
		typ := formatSqlServerType(dataType, charLen, numPrec, numScale)
		cols = append(cols, col{name, typ, strings.EqualFold(nullable, "YES"), colDef})
	}
	if len(cols) == 0 {
		return "", fmt.Errorf("table %q not found", table)
	}

	pkCols, err := d.PrimaryKeyColumns(ctx, database, schema, table)
	if err != nil {
		pkCols = nil
	}
	pkSet := make(map[string]struct{}, len(pkCols))
	for _, c := range pkCols {
		pkSet[strings.ToLower(c)] = struct{}{}
	}

	tblQual := mssqlQualName(db, schema, table)

	var lines []string
	for _, c := range cols {
		line := fmt.Sprintf("    %s %s", quoteBracket(c.name), c.typ)
		_, isPK := pkSet[strings.ToLower(c.name)]
		if !c.null && !isPK {
			line += " NOT NULL"
		}
		if c.def.Valid && strings.TrimSpace(c.def.String) != "" {
			line += " DEFAULT " + strings.TrimSpace(c.def.String)
		}
		lines = append(lines, line)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("CREATE TABLE %s (\n", tblQual))
	b.WriteString(strings.Join(lines, ",\n"))
	if len(pkCols) > 0 {
		var pks []string
		for _, c := range pkCols {
			pks = append(pks, quoteBracket(c))
		}
		b.WriteString(",\n    PRIMARY KEY (")
		b.WriteString(strings.Join(pks, ", "))
		b.WriteString(")")
	}
	b.WriteString("\n);\n")

	// Non-primary indexes (clustered/unique/nonclustered)
	idxRows, err := d.db.QueryContext(ctx, `
SELECT i.index_id, i.name, i.is_unique, ic.is_included_column, ic.key_ordinal, c.name AS colname
FROM sys.tables t
INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
INNER JOIN sys.indexes i ON t.object_id = i.object_id
INNER JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id
INNER JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id
WHERE s.name = @p1 AND t.name = @p2
  AND i.is_primary_key = 0
  AND i.type > 0
ORDER BY i.index_id, ic.key_ordinal, ic.index_column_id`, schema, table)
	if err == nil {
		type idxPart struct {
			keyOrd           int
			incl             bool
			name             string
			includedColOrder int
		}
		idxMap := make(map[int64]struct {
			name   string
			unique bool
			parts  []idxPart
		})
		for idxRows.Next() {
			var indexID int64
			var iname string
			var isUnique bool
			var isIncluded bool
			var keyOrd sql.NullInt32
			var cname string
			if err := idxRows.Scan(&indexID, &iname, &isUnique, &isIncluded, &keyOrd, &cname); err != nil {
				continue
			}
			ent := idxMap[indexID]
			if ent.name == "" {
				ent.name = iname
				ent.unique = isUnique
			}
			ko := 0
			if keyOrd.Valid {
				ko = int(keyOrd.Int32)
			}
			ent.parts = append(ent.parts, idxPart{keyOrd: ko, incl: isIncluded, name: cname})
			idxMap[indexID] = ent
		}
		idxRows.Close()

		var indexIDs []int64
		for id := range idxMap {
			indexIDs = append(indexIDs, id)
		}
		sort.Slice(indexIDs, func(i, j int) bool { return indexIDs[i] < indexIDs[j] })

		for _, id := range indexIDs {
			ent := idxMap[id]
			var keys, inc []string
			// Sort parts: key columns by key_ordinal, then included
			sort.SliceStable(ent.parts, func(i, j int) bool {
				pi, pj := ent.parts[i], ent.parts[j]
				if pi.incl != pj.incl {
					return !pi.incl
				}
				if pi.keyOrd != pj.keyOrd {
					return pi.keyOrd < pj.keyOrd
				}
				return pi.name < pj.name
			})
			for _, p := range ent.parts {
				if p.incl {
					inc = append(inc, quoteBracket(p.name))
				} else {
					keys = append(keys, quoteBracket(p.name))
				}
			}
			if len(keys) == 0 {
				continue
			}
			uq := ""
			if ent.unique {
				uq = "UNIQUE "
			}
			incClause := ""
			if len(inc) > 0 {
				incClause = " INCLUDE (" + strings.Join(inc, ", ") + ")"
			}
			b.WriteString(fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)%s;\n",
				uq, quoteBracket(ent.name), tblQual, strings.Join(keys, ", "), incClause))
		}
	}

	return strings.TrimSpace(b.String()), nil
}

func quoteBracket(ident string) string {
	return "[" + strings.ReplaceAll(ident, "]", "]]") + "]"
}

// mssqlQualName builds [db].[schema].[obj] or [schema].[obj] when db is empty.
func mssqlQualName(db, schema, obj string) string {
	s, o := quoteBracket(schema), quoteBracket(obj)
	if db != "" {
		return quoteBracket(db) + "." + s + "." + o
	}
	return s + "." + o
}

func formatSqlServerType(dataType string, charLen sql.NullInt64, numPrec, numScale sql.NullInt64) string {
	dt := strings.ToLower(dataType)
	switch dt {
	case "varchar", "nvarchar", "char", "nchar", "binary", "varbinary":
		if charLen.Valid {
			if charLen.Int64 < 0 {
				return strings.ToUpper(dt) + "(MAX)"
			}
			return fmt.Sprintf("%s(%d)", strings.ToUpper(dt), charLen.Int64)
		}
		return strings.ToUpper(dt)
	case "decimal", "numeric":
		if numPrec.Valid && numScale.Valid {
			return fmt.Sprintf("%s(%d,%d)", strings.ToUpper(dt), numPrec.Int64, numScale.Int64)
		}
		if numPrec.Valid {
			return fmt.Sprintf("%s(%d)", strings.ToUpper(dt), numPrec.Int64)
		}
		return strings.ToUpper(dt)
	case "float":
		if numPrec.Valid {
			return fmt.Sprintf("FLOAT(%d)", numPrec.Int64)
		}
		return "FLOAT"
	default:
		return strings.ToUpper(dt)
	}
}
