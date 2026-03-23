package results

import (
	"strings"
	"testing"
)

func TestTryPrettyJSONPreservesKeyOrder(t *testing.T) {
	in := `{"z":1,"a":2,"m":{"nested_z":0,"nested_a":1}}`
	out, ok := tryPrettyJSON(in)
	if !ok {
		t.Fatal("expected valid JSON")
	}
	zBeforeA := strings.Index(out, `"z"`) < strings.Index(out, `"a"`)
	if !zBeforeA {
		t.Errorf("top-level key order not preserved:\n%s", out)
	}
	nz := strings.Index(out, `"nested_z"`)
	na := strings.Index(out, `"nested_a"`)
	if nz < 0 || na < 0 || nz > na {
		t.Errorf("nested key order not preserved:\n%s", out)
	}
}

func TestTryPrettyJSONInvalid(t *testing.T) {
	if _, ok := tryPrettyJSON(`{`); ok {
		t.Fatal("expected invalid")
	}
}
