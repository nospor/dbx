package editor

import (
	"sort"
	"strings"
)

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
	if prefix == "" {
		return nil
	}
	orderedPools := p.poolsForContext(beforeCursor)
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

func (p *CompletionProvider) poolsForContext(beforeCursor string) [][]string {
	word, prev := lastTwoSQLWords(beforeCursor)
	switch {
	case word == "from" || word == "join" || word == "update" || word == "into":
		return [][]string{p.tables, p.columns, p.keywords, p.tokens}
	case word == "select" || word == "where" || word == "on" || word == "set" || word == "having":
		return [][]string{p.columns, p.tables, p.keywords, p.tokens}
	case word == "by" && (prev == "order" || prev == "group"):
		return [][]string{p.columns, p.tables, p.keywords, p.tokens}
	default:
		return [][]string{p.tokens}
	}
}

func filterByPrefix(tokens []string, prefix string) []string {
	if prefix == "" {
		return nil
	}
	lower := strings.ToLower(prefix)
	out := make([]string, 0, 8)
	for _, t := range tokens {
		if strings.HasPrefix(strings.ToLower(t), lower) {
			out = append(out, t)
		}
	}
	return out
}

func lastTwoSQLWords(s string) (string, string) {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_')
	})
	if len(fields) == 0 {
		return "", ""
	}
	if len(fields) == 1 {
		return fields[0], ""
	}
	return fields[len(fields)-1], fields[len(fields)-2]
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
