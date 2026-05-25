package sqlutil

import (
	"strings"
	"unicode"
)

// SplitSemicolonStatements splits SQL on semicolons that are not inside strings
// (” / "" / “) or parentheses.
func SplitSemicolonStatements(sql string) []string {
	s := sql
	var parts []string
	start := 0
	depth := 0
	inSingle, inDouble, inBacktick := false, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inSingle {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				inSingle = false
			}
			continue
		}
		if inDouble {
			if c == '"' {
				if i+1 < len(s) && s[i+1] == '"' {
					i++
					continue
				}
				inDouble = false
			}
			continue
		}
		if inBacktick {
			if c == '`' {
				if i+1 < len(s) && s[i+1] == '`' {
					i++
					continue
				}
				inBacktick = false
			}
			continue
		}
		switch c {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '`':
			inBacktick = true
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// SplitExecBatchDeleteUpdate splits on semicolons and returns statements
// only when every non-empty fragment (after stripping comments) is a DELETE or UPDATE.
// Comment-only fragments between statements are skipped.
// Returns ok=false if there are zero executable statements, or if any fragment is not DELETE/UPDATE.
func SplitExecBatchDeleteUpdate(sql string) ([]string, bool) {
	parts := SplitSemicolonStatements(sql)
	var out []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		core := strings.TrimSpace(StripSQLComments(t))
		if core == "" {
			continue
		}
		if !statementIsDeleteOrUpdate(core) {
			return nil, false
		}
		out = append(out, core)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func statementIsDeleteOrUpdate(s string) bool {
	u := strings.ToUpper(strings.TrimSpace(s))
	return sqlWordPrefix(u, "DELETE") || sqlWordPrefix(u, "UPDATE")
}

func sqlWordPrefix(u, word string) bool {
	if !strings.HasPrefix(u, word) {
		return false
	}
	if len(u) == len(word) {
		return true
	}
	next := rune(u[len(word)])
	return unicode.IsSpace(next) || next == '\n' || next == '\r' || next == '\t'
}
