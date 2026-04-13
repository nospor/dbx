package sqlutil

import "strings"

// WrapQueryForExplain returns SQL wrapped so the engine returns a query plan instead of
// (or in addition to) normal results. Returns ok false when query is empty or already
// appears to request a plan.
func WrapQueryForExplain(driver, query string) (wrapped string, ok bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", false
	}
	if queryAlreadyExplained(driver, query) {
		return "", false
	}
	d := strings.ToLower(strings.TrimSpace(driver))
	switch d {
	case "sqlite", "sqlite3":
		return "EXPLAIN QUERY PLAN " + query, true
	case "mssql", "sqlserver":
		return "SET SHOWPLAN_ALL ON;\n" + query + "\nSET SHOWPLAN_ALL OFF", true
	default:
		// postgres, mysql, and unknown drivers
		return "EXPLAIN " + query, true
	}
}

func queryAlreadyExplained(driver, query string) bool {
	t := strings.TrimSpace(strings.ToUpper(query))
	if strings.HasPrefix(t, "EXPLAIN") {
		return true
	}
	d := strings.ToLower(strings.TrimSpace(driver))
	if d == "mssql" || d == "sqlserver" {
		if strings.Contains(t, "SET SHOWPLAN_ALL ON") {
			return true
		}
	}
	return false
}
