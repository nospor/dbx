package results

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// tryPrettyJSON returns indented JSON while preserving object key order from the input.
func tryPrettyJSON(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	var buf bytes.Buffer
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return "", false
	}
	if err := writePrettyValue(&buf, dec, tok, 0); err != nil {
		return "", false
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", false
	}
	return buf.String(), true
}

func writePrettyValue(w io.Writer, dec *json.Decoder, tok json.Token, depth int) error {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return writePrettyObject(w, dec, depth)
		case '[':
			return writePrettyArray(w, dec, depth)
		default:
			return fmt.Errorf("unexpected delimiter %q in value", t)
		}
	case string:
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	case json.Number:
		_, err := io.WriteString(w, t.String())
		return err
	case bool:
		if t {
			_, err := w.Write([]byte("true"))
			return err
		}
		_, err := w.Write([]byte("false"))
		return err
	case nil:
		_, err := w.Write([]byte("null"))
		return err
	case float64:
		_, err := fmt.Fprintf(w, "%g", t)
		return err
	default:
		return fmt.Errorf("unknown JSON token type %T", tok)
	}
}

func writePrettyObject(w io.Writer, dec *json.Decoder, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); ok && d == '}' {
		_, err = w.Write([]byte("{}"))
		return err
	}
	key, ok := tok.(string)
	if !ok {
		return fmt.Errorf("expected object key string")
	}
	if _, err = w.Write([]byte("{\n")); err != nil {
		return err
	}
	indent := strings.Repeat("  ", depth+1)
	first := true
	for {
		if !first {
			if _, err = w.Write([]byte(",\n")); err != nil {
				return err
			}
		}
		first = false
		kb, err := json.Marshal(key)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(w, "%s%s: ", indent, kb); err != nil {
			return err
		}
		tok, err = dec.Token()
		if err != nil {
			return err
		}
		if err := writePrettyValue(w, dec, tok, depth+1); err != nil {
			return err
		}
		tok, err = dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok && d == '}' {
			_, err = fmt.Fprintf(w, "\n%s}", strings.Repeat("  ", depth))
			return err
		}
		key, ok = tok.(string)
		if !ok {
			return fmt.Errorf("expected object key or }")
		}
	}
}

func writePrettyArray(w io.Writer, dec *json.Decoder, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); ok && d == ']' {
		_, err = w.Write([]byte("[]"))
		return err
	}
	if _, err = w.Write([]byte("[\n")); err != nil {
		return err
	}
	indent := strings.Repeat("  ", depth+1)
	first := true
	for {
		if !first {
			if _, err = w.Write([]byte(",\n")); err != nil {
				return err
			}
		}
		first = false
		if _, err = w.Write([]byte(indent)); err != nil {
			return err
		}
		if err := writePrettyValue(w, dec, tok, depth+1); err != nil {
			return err
		}
		tok, err = dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok && d == ']' {
			_, err = fmt.Fprintf(w, "\n%s]", strings.Repeat("  ", depth))
			return err
		}
	}
}
