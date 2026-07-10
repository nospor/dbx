package sqlutil

import (
	"encoding/json"
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
	if strings.ToLower(driver) == "mongodb" {
		_, tbl := ParseTableRef(tableExpr, driver)
		var docs []string
		for _, row := range rows {
			q, err := mongoRowQuery(columns, row, columns)
			if err != nil {
				return "", err
			}
			docs = append(docs, "    "+q)
		}
		return fmt.Sprintf("{\n  \"insert\": %q,\n  \"documents\": [\n%s\n  ]\n}", tbl, strings.Join(docs, ",\n")), nil
	}
	if strings.ToLower(driver) == "elasticsearch" {
		// Generate ndjson bulk index payload
		var sb strings.Builder
		for _, row := range rows {
			// Build the document map from columns/row
			docMap := make(map[string]interface{}, len(columns))
			id := ""
			for i, col := range columns {
				val := ""
				if i < len(row) {
					val = row[i]
				}
				if col == "_id" {
					id = val
					continue
				}
				docMap[col] = val
			}
			// Action line
			if id != "" {
				sb.WriteString(fmt.Sprintf("{\"index\":{\"_index\":%q,\"_id\":%q}}\n", tableExpr, id))
			} else {
				sb.WriteString(fmt.Sprintf("{\"index\":{\"_index\":%q}}\n", tableExpr))
			}
			docJSON, _ := json.Marshal(docMap)
			sb.Write(docJSON)
			sb.WriteByte('\n')
		}
		return strings.TrimSpace(sb.String()), nil
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
