package sqlutil

import (
	"strings"
	"unicode"
)

// ParseTableRef extracts (schema, table) for catalog lookups from a FROM expression
// as returned by TableFromSimpleSelect (may include quoting).
// database is unused for most drivers; MySQL uses schema="" to mean "use connection database".
func ParseTableRef(expr string, driver string) (schema, table string) {
	parts := splitTableParts(strings.TrimSpace(expr))
	d := strings.ToLower(driver)

	switch d {
	case "mysql":
		switch len(parts) {
		case 0:
			return "", ""
		case 1:
			return "", parts[0]
		default:
			return parts[len(parts)-2], parts[len(parts)-1]
		}
	case "mssql", "sqlserver":
		switch len(parts) {
		case 0:
			return "", ""
		case 1:
			return "dbo", parts[0]
		case 2:
			return parts[0], parts[1]
		default:
			// [catalog].[schema].[name] -> use last two
			return parts[len(parts)-2], parts[len(parts)-1]
		}
	case "sqlite", "sqlite3":
		if len(parts) == 0 {
			return "", ""
		}
		return "", parts[len(parts)-1]
	default: // postgres, postgresql
		switch len(parts) {
		case 0:
			return "", ""
		case 1:
			return "public", parts[0]
		case 2:
			return parts[0], parts[1]
		default:
			return parts[len(parts)-2], parts[len(parts)-1]
		}
	}
}

// splitTableParts splits a.b, `a`.`b`, "a"."b", [a].[b] into unquoted segments.
func splitTableParts(expr string) []string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	var parts []string
	for len(expr) > 0 {
		expr = strings.TrimSpace(expr)
		if expr == "" {
			break
		}
		var seg string
		switch expr[0] {
		case '`':
			end := scanQuoted(expr, '`')
			if end < 0 {
				return parts
			}
			seg = unquoteBacktick(expr[:end])
			expr = expr[end:]
		case '"':
			end := scanQuoted(expr, '"')
			if end < 0 {
				return parts
			}
			seg = unquoteDouble(expr[:end])
			expr = expr[end:]
		case '[':
			j := 1
			for j < len(expr) && expr[j] != ']' {
				j++
			}
			if j >= len(expr) {
				return parts
			}
			seg = strings.ReplaceAll(expr[1:j], "]]", "]")
			expr = expr[j+1:]
		default:
			i := 0
			runes := []rune(expr)
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_' || runes[i] == '$' || runes[i] == '#') {
				i++
			}
			if i == 0 {
				return parts
			}
			seg = string(runes[:i])
			expr = string(runes[i:])
		}
		parts = append(parts, seg)
		expr = strings.TrimSpace(expr)
		if len(expr) > 0 && expr[0] == '.' {
			expr = expr[1:]
		}
	}
	return parts
}

func unquoteBacktick(s string) string {
	if len(s) >= 2 && s[0] == '`' {
		s = strings.TrimPrefix(s, "`")
		s = strings.TrimSuffix(s, "`")
	}
	return strings.ReplaceAll(s, "``", "`")
}

func unquoteDouble(s string) string {
	if len(s) >= 2 && s[0] == '"' {
		s = strings.TrimPrefix(s, `"`)
		s = strings.TrimSuffix(s, `"`)
	}
	return strings.ReplaceAll(s, `""`, `"`)
}
