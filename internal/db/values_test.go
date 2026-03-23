package db

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestFormatPGValueBytea(t *testing.T) {
	bin := []byte{0xff, 0, 0x48, 0x69}
	got := formatPGValue(pgtype.ByteaOID, bin)
	want := `\xff004869`
	if got != want {
		t.Fatalf("bytea: got %q want %q", got, want)
	}
}

func TestFormatPGValueByteaEmpty(t *testing.T) {
	got := formatPGValue(pgtype.ByteaOID, []byte{})
	if got != `\x` {
		t.Fatalf("empty bytea: got %q", got)
	}
}

func TestFormatPGValueTextAsBytes(t *testing.T) {
	got := formatPGValue(0, []byte("hello"))
	if got != "hello" {
		t.Fatalf("got %q want hello", got)
	}
}

func TestFormatPGValueUUID(t *testing.T) {
	var u [16]byte
	copy(u[:], []byte{0x6b, 0xa7, 0xb8, 0x10, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8})
	got := formatPGValue(pgtype.UUIDOID, u)
	want := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	if got != want {
		t.Fatalf("uuid: got %q want %q", got, want)
	}
}

func TestDatabaseTypeNameLooksBinary(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"BLOB", true},
		{"VARBINARY", true},
		{"LONGTEXT", false},
		{"VARCHAR", false},
		{"TEXT", false},
		{"BYTEA", true},
	}
	for _, tt := range tests {
		if got := databaseTypeNameLooksBinary(tt.name); got != tt.want {
			t.Errorf("%q: got %v want %v", tt.name, got, tt.want)
		}
	}
}

func TestFormatSQLValueWithColumnTextBytes(t *testing.T) {
	got := formatSQLValueWithColumn(nil, []byte("hello"))
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestTryDecodePGBinaryTimestampSample(t *testing.T) {
	// Example 8-byte PostgreSQL binary timestamp (same shape as user-reported \x hex).
	b := []byte{0, 0, 0, 0, 0x0e, 0x38, 0x7d, 0x52}
	s, ok := tryDecodePGBinary(pgtype.TimestampOID, b)
	if !ok {
		t.Fatal("expected binary timestamp decode")
	}
	if s == "" || strings.HasPrefix(s, `\x`) {
		t.Fatalf("unexpected output %q", s)
	}
}

func TestFormatPGValueByteaDecodesTimestampBinary(t *testing.T) {
	b := []byte{0, 0, 0, 0, 0x0e, 0x38, 0x7d, 0x52}
	got := formatPGValue(pgtype.ByteaOID, b)
	if strings.HasPrefix(got, `\x`) {
		t.Fatalf("bytea column should try timestamp binary first: got %q", got)
	}
}
