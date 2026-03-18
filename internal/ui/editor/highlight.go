package editor

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/robertn/dbx/internal/ui/theme"
)

var sqlLexer chroma.Lexer

func init() {
	sqlLexer = lexers.Get("sql")
	if sqlLexer == nil {
		sqlLexer = lexers.Fallback
	}
	sqlLexer = chroma.Coalesce(sqlLexer)
}

// renderHighlighted returns a syntax-highlighted version of a SQL line using ANSI codes.
// Falls back to plain text if highlighting fails.
func renderHighlighted(line string, _ theme.Theme) string {
	if strings.TrimSpace(line) == "" {
		return line
	}

	iterator, err := sqlLexer.Tokenise(nil, line)
	if err != nil {
		return line
	}

	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}

	var sb strings.Builder
	for token := iterator(); token != chroma.EOF; token = iterator() {
		color := style.Get(token.Type)
		text := token.Value
		if color.Colour.IsSet() {
			r, g, b := color.Colour.Red(), color.Colour.Green(), color.Colour.Blue()
			// ANSI true-color escape
			sb.WriteString("\x1b[38;2;")
			sb.WriteString(itoa(int(r)))
			sb.WriteString(";")
			sb.WriteString(itoa(int(g)))
			sb.WriteString(";")
			sb.WriteString(itoa(int(b)))
			sb.WriteString("m")
			sb.WriteString(text)
			sb.WriteString("\x1b[0m")
		} else {
			sb.WriteString(text)
		}
	}
	return sb.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [10]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
