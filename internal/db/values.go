package db

import "fmt"

// formatSQLValue converts scanned DB values into displayable strings.
func formatSQLValue(v interface{}) string {
	if v == nil {
		return "NULL"
	}
	switch x := v.(type) {
	case []byte:
		return string(x)
	default:
		return fmt.Sprintf("%v", v)
	}
}
