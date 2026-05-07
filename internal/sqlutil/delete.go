package sqlutil

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	reInt   = regexp.MustCompile(`^-?[0-9]+$`)
	reFloat = regexp.MustCompile(`^-?[0-9]+\.[0-9]+$`)
)

// DeleteForRows builds DELETE statements (one per row).
// If whereColumns is non-empty, the WHERE clause uses only those names (each must match
// a name in columns, case-insensitively). Otherwise all columns are used.
// tableExpr is pasted verbatim after DELETE FROM (as taken from the original SELECT).
func DeleteForRows(driver, tableExpr string, columns []string, rows [][]string, whereColumns []string) (string, error) {
	if tableExpr == "" {
		return "", fmt.Errorf("empty table")
	}
	if len(columns) == 0 {
		return "", fmt.Errorf("no columns")
	}
	if strings.ToLower(driver) == "mongodb" {
		_, tbl := ParseTableRef(tableExpr, driver)
		var deletes []string
		wc := whereColumns
		if len(wc) == 0 {
			wc = columns
		}
		for _, row := range rows {
			q, err := mongoRowQuery(columns, row, wc)
			if err != nil {
				return "", err
			}
			deletes = append(deletes, fmt.Sprintf("    {\"q\": %s, \"limit\": 1}", q))
		}
		return fmt.Sprintf("{\n  \"delete\": %q,\n  \"deletes\": [\n%s\n  ]\n}", tbl, strings.Join(deletes, ",\n")), nil
	}
	wc := whereColumns
	if len(wc) == 0 {
		wc = columns
	}
	var b strings.Builder
	for _, row := range rows {
		where, err := rowWhereClause(driver, columns, row, wc)
		if err != nil {
			return "", err
		}
		b.WriteString("DELETE FROM ")
		b.WriteString(tableExpr)
		b.WriteString(" WHERE ")
		b.WriteString(where)
		b.WriteString(";\n")
	}
	return strings.TrimSpace(b.String()), nil
}

// MatchResultColumnsForPK maps PK names to result column names (exact spelling from result).
// Returns nil if any PK column is missing from the result set.
func MatchResultColumnsForPK(resultCols []string, pkNames []string) []string {
	if len(pkNames) == 0 {
		return nil
	}
	byLower := make(map[string]string, len(resultCols))
	for _, c := range resultCols {
		byLower[strings.ToLower(c)] = c
	}
	out := make([]string, 0, len(pkNames))
	for _, pk := range pkNames {
		if act, ok := byLower[strings.ToLower(pk)]; ok {
			out = append(out, act)
		} else {
			return nil
		}
	}
	return out
}

func rowWhereClause(driver string, columns []string, row []string, whereNames []string) (string, error) {
	idx := make(map[string]int, len(columns))
	for i, c := range columns {
		idx[strings.ToLower(c)] = i
	}
	parts := make([]string, 0, len(whereNames))
	for _, col := range whereNames {
		i, ok := idx[strings.ToLower(col)]
		if !ok {
			return "", fmt.Errorf("column %q not in result set", col)
		}
		val := ""
		if i < len(row) {
			val = row[i]
		}
		actName := columns[i]
		qcol, err := quoteIdent(driver, actName)
		if err != nil {
			return "", err
		}
		if strings.EqualFold(val, "NULL") {
			parts = append(parts, qcol+" IS NULL")
			continue
		}
		lit, err := formatLiteral(driver, val)
		if err != nil {
			return "", err
		}
		parts = append(parts, qcol+" = "+lit)
	}
	return strings.Join(parts, " AND "), nil
}

func quoteIdent(driver, ident string) (string, error) {
	if ident == "" {
		return "", fmt.Errorf("empty column name")
	}
	d := strings.ToLower(driver)
	switch d {
	case "mysql":
		esc := strings.ReplaceAll(ident, "`", "``")
		return "`" + esc + "`", nil
	case "mssql", "sqlserver":
		esc := strings.ReplaceAll(ident, "]", "]]")
		return "[" + esc + "]", nil
	default: // postgres, sqlite, postgresql, sqlite3
		esc := strings.ReplaceAll(ident, `"`, `""`)
		return `"` + esc + `"`, nil
	}
}

// UpdateForRow builds a single UPDATE statement setting colName = newValue.
// WHERE clause uses whereColumns if non-empty, otherwise all columns (same logic as DELETE).
func UpdateForRow(driver, tableExpr string, columns []string, row []string, colName, newValue string, whereColumns []string) (string, error) {
	if tableExpr == "" {
		return "", fmt.Errorf("empty table")
	}
	if len(columns) == 0 {
		return "", fmt.Errorf("no columns")
	}
	if strings.ToLower(driver) == "mongodb" {
		_, tbl := ParseTableRef(tableExpr, driver)
		wc := whereColumns
		if len(wc) == 0 {
			wc = columns
		}
		q, err := mongoRowQuery(columns, row, wc)
		if err != nil {
			return "", err
		}
		u := fmt.Sprintf("{\"$set\": {%q: %s}}", colName, mongoFormatValue(newValue))
		return fmt.Sprintf("{\n  \"update\": %q,\n  \"updates\": [\n    {\"q\": %s, \"u\": %s}\n  ]\n}", tbl, q, u), nil
	}
	qcol, err := quoteIdent(driver, colName)
	if err != nil {
		return "", err
	}
	var setExpr string
	if strings.EqualFold(newValue, "NULL") {
		setExpr = qcol + " = NULL"
	} else {
		lit, err := formatLiteral(driver, newValue)
		if err != nil {
			return "", err
		}
		setExpr = qcol + " = " + lit
	}
	wc := whereColumns
	if len(wc) == 0 {
		wc = columns
	}
	where, err := rowWhereClause(driver, columns, row, wc)
	if err != nil {
		return "", err
	}
	return "UPDATE " + tableExpr + " SET " + setExpr + " WHERE " + where + ";", nil
}

func formatLiteral(driver, s string) (string, error) {
	if reInt.MatchString(s) || reFloat.MatchString(s) {
		return s, nil
	}
	d := strings.ToLower(driver)
	switch d {
	case "mysql":
		esc := strings.ReplaceAll(s, "\\", "\\\\")
		esc = strings.ReplaceAll(esc, "'", "''")
		return "'" + esc + "'", nil
	default:
		esc := strings.ReplaceAll(s, "'", "''")
		return "'" + esc + "'", nil
	}
}

func mongoFormatValue(val string) string {
	if val == "null" || strings.EqualFold(val, "NULL") {
		return "null"
	}
	valTrimmed := strings.TrimSpace(val)
	if (strings.HasPrefix(valTrimmed, "{") && strings.HasSuffix(valTrimmed, "}")) ||
		(strings.HasPrefix(valTrimmed, "[") && strings.HasSuffix(valTrimmed, "]")) {
		if json.Valid([]byte(valTrimmed)) {
			return valTrimmed
		}
	}
	if reInt.MatchString(valTrimmed) || reFloat.MatchString(valTrimmed) {
		return valTrimmed
	}
	b, _ := json.Marshal(val)
	return string(b)
}

func mongoRowQuery(columns []string, row []string, whereNames []string) (string, error) {
	idx := make(map[string]int, len(columns))
	for i, c := range columns {
		idx[strings.ToLower(c)] = i
	}
	parts := make([]string, 0, len(whereNames))
	for _, col := range whereNames {
		i, ok := idx[strings.ToLower(col)]
		if !ok {
			return "", fmt.Errorf("column %q not in result set", col)
		}
		val := ""
		if i < len(row) {
			val = row[i]
		}
		parts = append(parts, fmt.Sprintf("%q: %s", columns[i], mongoFormatValue(val)))
	}
	return "{" + strings.Join(parts, ", ") + "}", nil
}
