package sqlutil

import (
	"encoding/json"
	"strings"
	"unicode"
)

// TableFromSimpleSelect extracts the table reference immediately after the first top-level FROM
// for straightforward single-table SELECTs (no subquery in FROM, no JOIN, no comma-FROM).
// The returned string is copied verbatim from the original SQL (including any quoting).
func TableFromSimpleSelect(sql string) (string, bool) {
	s := StripSQLComments(sql)
	s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
	if s == "" {
		return "", false
	}
	u := strings.ToUpper(s)
	// CTE queries need a fuller parser; require a plain SELECT for delete generation.
	if strings.HasPrefix(u, "WITH") {
		return "", false
	}
	if !strings.HasPrefix(u, "SELECT") {
		return "", false
	}

	fromPos := -1
	depth := 0
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inSingle {
			if c == '\'' && (i+1 >= len(s) || s[i+1] != '\'') {
				inSingle = false
			} else if c == '\'' && i+1 < len(s) && s[i+1] == '\'' {
				i++
			}
			continue
		}
		if inDouble {
			if c == '"' && (i+1 >= len(s) || s[i+1] != '"') {
				inDouble = false
			} else if c == '"' && i+1 < len(s) && s[i+1] == '"' {
				i++
			}
			continue
		}
		switch c {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if depth != 0 {
			continue
		}
		if i+3 < len(s) && strings.EqualFold(s[i:i+4], "from") {
			// word boundary before FROM
			if i > 0 && isIdentCont(rune(s[i-1])) {
				continue
			}
			j := i + 4
			for j < len(s) && unicode.IsSpace(rune(s[j])) {
				j++
			}
			if j >= len(s) || s[j] == '(' {
				continue
			}
			fromPos = j
			break
		}
	}
	if fromPos < 0 {
		return "", false
	}

	rest := strings.TrimSpace(s[fromPos:])
	if len(rest) == 0 || rest[0] == '(' {
		return "", false
	}

	// Optional LATERAL / TABLE (...) — only skip simple LATERAL keyword
	if hasPrefixFold(rest, "lateral ") {
		rest = strings.TrimSpace(rest[len("lateral "):])
	}

	table, tail := readFromTable(rest)
	if table == "" {
		return "", false
	}
	tail = strings.TrimSpace(tail)
	if len(tail) > 0 {
		switch {
		case tail[0] == ',':
			return "", false
		case hasPrefixFold(tail, "join"):
			return "", false
		case hasPrefixFold(tail, "cross"):
			return "", false
		case hasPrefixFold(tail, "inner"):
			return "", false
		case hasPrefixFold(tail, "left"):
			return "", false
		case hasPrefixFold(tail, "right"):
			return "", false
		case hasPrefixFold(tail, "full"):
			return "", false
		case hasPrefixFold(tail, "natural"):
			return "", false
		}
	}
	return table, true
}

func hasPrefixFold(s, p string) bool {
	return len(s) >= len(p) && strings.EqualFold(s[:len(p)], p)
}

func isIdentCont(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func readFromTable(rest string) (table, tail string) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", ""
	}
	runes := []rune(rest)

	switch {
	case len(runes) > 0 && runes[0] == '`':
		pos, ok := scanChainedQuoted(rest, '`')
		if !ok {
			return "", ""
		}
		return strings.TrimSpace(rest[:pos]), strings.TrimSpace(rest[pos:])
	case len(runes) > 0 && runes[0] == '"':
		pos, ok := scanChainedQuoted(rest, '"')
		if !ok {
			return "", ""
		}
		return strings.TrimSpace(rest[:pos]), strings.TrimSpace(rest[pos:])
	case len(runes) > 0 && runes[0] == '[':
		end := scanBracketTable(rest)
		if end < 0 {
			return "", ""
		}
		table = rest[:end]
		return table, strings.TrimSpace(rest[end:])
	default:
		i := 0
		for i < len(runes) {
			r := runes[i]
			if r == '.' && i+1 < len(runes) {
				next := runes[i+1]
				if isIdentStart(next) || next == '`' || next == '"' || next == '[' {
					i++
					continue
				}
			}
			if isIdentStart(r) || unicode.IsDigit(r) {
				i++
				continue
			}
			break
		}
		if i == 0 {
			return "", ""
		}
		table = strings.TrimSpace(string(runes[:i]))
		return table, strings.TrimSpace(string(runes[i:]))
	}
}

func isIdentStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_' || r == '#'
}

// scanChainedQuoted reads `a`.`b` / "a"."b" style chains; returns end byte index in rest & ok.
func scanChainedQuoted(rest string, q byte) (int, bool) {
	pos := 0
	for pos < len(rest) && rest[pos] == q {
		e := scanQuoted(rest[pos:], q)
		if e < 0 {
			return 0, false
		}
		pos += e
		if pos < len(rest) && rest[pos] == '.' {
			pos++
			continue
		}
		break
	}
	if pos == 0 {
		return 0, false
	}
	return pos, true
}

func scanQuoted(s string, q byte) int {
	if len(s) < 2 || s[0] != q {
		return -1
	}
	i := 1
	for i < len(s) {
		if s[i] == q {
			if i+1 < len(s) && s[i+1] == q {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return -1
}

// scanBracketTable reads [dbo].[table] style segments until a non-bracket tail.
func scanBracketTable(s string) int {
	i := 0
	for i < len(s) && s[i] == '[' {
		j := i + 1
		for j < len(s) && s[j] != ']' {
			j++
		}
		if j >= len(s) {
			return -1
		}
		i = j + 1
		// Optional dot before next bracket
		if i < len(s) && s[i] == '.' {
			i++
		}
	}
	return i
}

// StripSQLComments removes -- line comments and /* */ block comments.
func StripSQLComments(sql string) string {
	var out strings.Builder
	i := 0
	for i < len(sql) {
		if i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-' {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*' {
			i += 2
			for i+1 < len(sql) {
				if sql[i] == '*' && sql[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		out.WriteByte(sql[i])
		i++
	}
	return out.String()
}

// CollectionFromMongoCommand extracts the collection name from a raw JSON command.
// It looks for "find", "aggregate", "insert", "update", or "delete" keys.
func CollectionFromMongoCommand(cmd string) (string, bool) {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(cmd), &m); err != nil {
		return "", false
	}
	keys := []string{"find", "aggregate", "insert", "update", "delete"}
	for _, k := range keys {
		if v, ok := m[k].(string); ok {
			return v, true
		}
	}
	return "", false
}

// IndexFromElasticCommand extracts the index name from an Elasticsearch command.
//
// It supports:
//   - Shorthand verb lines: GET /index/_search, POST /index/_doc, etc.
//   - Bare JSON objects (returns "" because no index is embedded in the body).
func IndexFromElasticCommand(cmd string) (string, bool) {
	trimmed := strings.TrimSpace(cmd)
	upper := strings.ToUpper(trimmed)
	for _, verb := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD"} {
		if strings.HasPrefix(upper, verb+" ") || strings.HasPrefix(upper, verb+"\t") {
			rest := strings.TrimSpace(trimmed[len(verb):])
			// rest starts with /index/... or index/...
			if len(rest) == 0 {
				return "", false
			}
			// Strip leading slash
			if rest[0] == '/' {
				rest = rest[1:]
			}
			// Take the first path segment as index
			end := strings.IndexAny(rest, "/ \t\n")
			if end < 0 {
				return rest, rest != ""
			}
			idx := rest[:end]
			return idx, idx != ""
		}
	}
	return "", false
}
