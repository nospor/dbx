package editor

import (
	"sort"
	"strings"
)

// CompletionProvider holds schema tokens for autocomplete.
type CompletionProvider struct {
	tokens []string // sorted list of table/column/keyword names
}

// NewCompletionProvider creates a provider with SQL keywords pre-loaded.
func NewCompletionProvider() *CompletionProvider {
	return &CompletionProvider{tokens: sqlKeywords()}
}

// SetSchema replaces the schema tokens (tables + columns).
func (p *CompletionProvider) SetSchema(tables, columns []string) {
	all := make([]string, 0, len(sqlKeywords())+len(tables)+len(columns))
	all = append(all, sqlKeywords()...)
	all = append(all, tables...)
	all = append(all, columns...)
	sort.Strings(all)
	p.tokens = all
}

// Complete returns up to maxResults completions for the given prefix.
func (p *CompletionProvider) Complete(prefix string, maxResults int) []string {
	if prefix == "" {
		return nil
	}
	lower := strings.ToLower(prefix)
	var matches []string
	for _, t := range p.tokens {
		if strings.HasPrefix(strings.ToLower(t), lower) {
			matches = append(matches, t)
			if len(matches) >= maxResults {
				break
			}
		}
	}
	return matches
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
