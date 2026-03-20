package editor

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
)

// foldCaser matches prefixes case-insensitively (Unicode-aware).
var foldCaser = cases.Fold()

// CompletionProvider holds schema tokens for autocomplete.
type CompletionProvider struct {
	tokens   []string // sorted list of all tokens
	keywords []string
	tables   []string
	columns  []string
}

// NewCompletionProvider creates a provider with SQL keywords pre-loaded.
func NewCompletionProvider() *CompletionProvider {
	kws := sqlKeywords()
	sort.Strings(kws)
	return &CompletionProvider{
		tokens:   kws,
		keywords: kws,
		tables:   []string{},
		columns:  []string{},
	}
}

// SetSchema replaces the schema tokens (tables + columns).
func (p *CompletionProvider) SetSchema(tables, columns []string) {
	p.tables = append([]string(nil), tables...)
	p.columns = append([]string(nil), columns...)
	sort.Strings(p.tables)
	sort.Strings(p.columns)
	all := make([]string, 0, len(sqlKeywords())+len(tables)+len(columns))
	all = append(all, sqlKeywords()...)
	all = append(all, tables...)
	all = append(all, columns...)
	sort.Strings(all)
	p.tokens = all
}

// Complete returns up to maxResults completions for the given prefix.
func (p *CompletionProvider) Complete(prefix string, maxResults int) []string {
	return p.CompleteWithContext(prefix, "", maxResults)
}

// CompleteWithContext prefers token categories based on SQL context around cursor.
func (p *CompletionProvider) CompleteWithContext(prefix, beforeCursor string, maxResults int) []string {
	orderedPools := p.poolsForContext(beforeCursor)
	if prefix == "" {
		return firstTokensFromPools(orderedPools, maxResults)
	}
	seen := make(map[string]struct{}, maxResults)
	matches := make([]string, 0, maxResults)
	for _, pool := range orderedPools {
		for _, t := range filterByPrefix(pool, prefix) {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			matches = append(matches, t)
			if len(matches) >= maxResults {
				return matches
			}
		}
	}
	return matches
}

// firstTokensFromPools returns up to maxResults distinct tokens from pools in order (for Tab with no typed prefix).
func firstTokensFromPools(pools [][]string, maxResults int) []string {
	seen := make(map[string]struct{}, maxResults)
	out := make([]string, 0, maxResults)
	for _, pool := range pools {
		for _, t := range pool {
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
			if len(out) >= maxResults {
				return out
			}
		}
	}
	return out
}

func (p *CompletionProvider) poolsForContext(beforeCursor string) [][]string {
	prevClause, _, alone := clauseBeforePartial(beforeCursor)

	switch alone {
	case "select":
		return [][]string{p.columns, p.tables, p.keywords, p.tokens}
	case "from", "join":
		return [][]string{p.tables, p.columns, p.keywords, p.tokens}
	case "where":
		return [][]string{p.columns, p.tables, p.keywords, p.tokens}
	case "insert":
		return [][]string{p.keywords, p.tables, p.columns, p.tokens}
	case "update":
		return [][]string{p.tables, p.columns, p.keywords, p.tokens}
	}

	switch prevClause {
	case "from", "join", "update", "into":
		return [][]string{p.tables, p.columns, p.keywords, p.tokens}
	case "select", "where", "on", "set", "having":
		return [][]string{p.columns, p.tables, p.keywords, p.tokens}
	}

	fields := sqlFieldsBeforeCursor(beforeCursor)
	if len(fields) >= 2 {
		a, b := fields[len(fields)-2], fields[len(fields)-1]
		if a == "order" && b == "by" || a == "group" && b == "by" {
			return [][]string{p.columns, p.tables, p.keywords, p.tokens}
		}
	}

	return [][]string{p.tokens}
}

// clauseBeforePartial inspects tokens before the cursor. The last token is treated as the
// partial identifier being typed; the previous token or the last clause keyword decides context.
func clauseBeforePartial(beforeCursor string) (prevClause string, partial string, aloneClause string) {
	fields := sqlFieldsBeforeCursor(beforeCursor)
	if len(fields) == 0 {
		return "", "", ""
	}
	if len(fields) == 1 {
		w := fields[0]
		if isClauseKeyword(w) {
			return "", "", w
		}
		return "", w, ""
	}
	last := fields[len(fields)-1]
	prefixFields := fields[:len(fields)-1]
	secondLast := prefixFields[len(prefixFields)-1]
	if isClauseKeyword(secondLast) {
		return secondLast, last, ""
	}
	return lastClauseKeyword(prefixFields), last, ""
}

func lastClauseKeyword(fields []string) string {
	for i := len(fields) - 1; i >= 0; i-- {
		if isClauseKeyword(fields[i]) {
			return fields[i]
		}
	}
	return ""
}

func isClauseKeyword(w string) bool {
	switch strings.ToLower(w) {
	case "select", "from", "join", "where", "on", "set", "having", "update", "into", "insert":
		return true
	default:
		return false
	}
}

func sqlFieldsBeforeCursor(s string) []string {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_'
	})
}

func filterByPrefix(tokens []string, prefix string) []string {
	if prefix == "" {
		return nil
	}
	prefFold := foldCaser.String(prefix)
	out := make([]string, 0, 8)
	for _, t := range tokens {
		if strings.HasPrefix(foldCaser.String(t), prefFold) {
			out = append(out, t)
		}
	}
	return out
}

func sqlKeywords() []string {
	return []string{
		"SELECT", "FROM", "WHERE", "AND", "OR", "NOT", "IN", "IS", "NULL",
		"INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE",
		"CREATE", "TABLE", "VIEW", "INDEX", "DROP", "ALTER", "ADD", "COLUMN",
		"JOIN", "INNER", "LEFT", "RIGHT", "OUTER", "FULL", "CROSS", "ON",
		"GROUP", "BY", "ORDER", "HAVING", "LIMIT", "OFFSET",
		"DISTINCT", "AS", "CASE", "WHEN", "THEN", "ELSE", "END",
		"COUNT", "SUM", "AVG", "MIN", "MAX",
		"BEGIN", "COMMIT", "ROLLBACK", "TRANSACTION",
		"PRIMARY", "KEY", "FOREIGN", "REFERENCES", "UNIQUE", "DEFAULT",
		"VARCHAR", "TEXT", "INTEGER", "INT", "BIGINT", "BOOLEAN", "BOOL",
		"FLOAT", "DOUBLE", "DECIMAL", "DATE", "TIMESTAMP", "SERIAL",
	}
}
