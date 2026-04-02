package sqlutil

import (
	"fmt"
	"strings"
)

// InsertForRows builds INSERT statements (one per row) using result column names
// and cell values. Identifiers and string literals are quoted for the driver.
// tableExpr is pasted verbatim after INSERT INTO (as from the original SELECT).
func InsertForRows(driver, tableExpr string, columns []string, rows [][]string) (string, error) {
	if tableExpr == "" {
		return "", fmt.Errorf("empty table")
	}
	if len(columns) == 0 {
		return "", fmt.Errorf("no columns")
	}
	var colList strings.Builder
	colList.WriteString("(")
	for i, col := range columns {
		if i > 0 {
			colList.WriteString(", ")
		}
		q, err := quoteIdent(driver, col)
		if err != nil {
			return "", err
		}
		colList.WriteString(q)
	}
	colList.WriteString(")")

	var b strings.Builder
	for _, row := range rows {
		values, err := rowValuesClause(driver, columns, row)
		if err != nil {
			return "", err
		}
		b.WriteString("INSERT INTO ")
		b.WriteString(tableExpr)
		b.WriteString(" ")
		b.WriteString(colList.String())
		b.WriteString(" VALUES ")
		b.WriteString(values)
		b.WriteString(";\n")
	}
	return strings.TrimSpace(b.String()), nil
}

func rowValuesClause(driver string, columns []string, row []string) (string, error) {
	parts := make([]string, 0, len(columns))
	for i := range columns {
		val := ""
		if i < len(row) {
			val = row[i]
		}
		if strings.EqualFold(val, "NULL") {
			parts = append(parts, "NULL")
			continue
		}
		lit, err := formatLiteral(driver, val)
		if err != nil {
			return "", err
		}
		parts = append(parts, lit)
	}
	return "(" + strings.Join(parts, ", ") + ")", nil
}
