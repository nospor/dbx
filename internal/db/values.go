package db

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// pgBinaryMap decodes PostgreSQL binary wire values (e.g. timestamp) from raw []byte.
var pgBinaryMap = pgtype.NewMap()

func tryDecodePGBinary(oid uint32, b []byte) (string, bool) {
	if len(b) == 0 || oid == 0 {
		return "", false
	}
	t, ok := pgBinaryMap.TypeForOID(oid)
	if !ok {
		return "", false
	}
	v, err := t.Codec.DecodeValue(pgBinaryMap, oid, pgtype.BinaryFormatCode, b)
	if err != nil {
		return "", false
	}
	return formatPGBinaryDecoded(v), true
}

func formatPGBinaryDecoded(v interface{}) string {
	if v == nil {
		return "NULL"
	}
	switch x := v.(type) {
	case time.Time:
		return x.String()
	case pgtype.InfinityModifier:
		return x.String()
	case [16]byte:
		// pgx DecodeValue for uuid returns raw bytes.
		return formatUUIDBytes(x)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatByteaDisplay renders binary data as PostgreSQL-style \x hex (safe for TUI).
func formatByteaDisplay(b []byte) string {
	if len(b) == 0 {
		return `\x`
	}
	return `\x` + hex.EncodeToString(b)
}

func formatUUIDBytes(b [16]byte) string {
	var buf [36]byte
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:], b[10:])
	return string(buf[:])
}

// formatPGValue converts a value from pgx Rows.Values() using PostgreSQL type OIDs.
// Text types decode as string; bytea is []byte and must not be interpreted as UTF-8.
// UUID columns decode as [16]byte from pgx DecodeValue.
func formatPGValue(oid uint32, v interface{}) string {
	if v == nil {
		return "NULL"
	}
	switch x := v.(type) {
	case []byte:
		// Typed column: decode PostgreSQL binary format (timestamp, timestamptz, etc.)
		if oid != pgtype.ByteaOID {
			if s, ok := tryDecodePGBinary(oid, x); ok {
				return s
			}
			return string(x)
		}
		// bytea: try timestamp-shaped payloads (8-byte PG binary) before showing hex
		if s, ok := tryDecodePGBinary(pgtype.TimestampOID, x); ok {
			return s
		}
		if s, ok := tryDecodePGBinary(pgtype.TimestamptzOID, x); ok {
			return s
		}
		return formatByteaDisplay(x)
	case [16]byte:
		if oid == pgtype.UUIDOID {
			return formatUUIDBytes(x)
		}
		return fmt.Sprintf("%v", x)
	case net.IP:
		return x.String()
	case pgtype.Numeric:
		if !x.Valid {
			return "NULL"
		}
		val, err := x.Value()
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		if val == nil {
			return "NULL"
		}
		return fmt.Sprintf("%v", val)
	case *pgtype.Numeric:
		if x == nil || !x.Valid {
			return "NULL"
		}
		val, err := x.Value()
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		if val == nil {
			return "NULL"
		}
		return fmt.Sprintf("%v", val)
	default:
		return formatSQLValue(v)
	}
}

// sqlColumnLooksBinary uses database/sql column metadata so []byte from
// MySQL/SQLite/MSSQL BLOB/VARBINARY is shown as hex, not as mis-decoded text.
func sqlColumnLooksBinary(ct *sql.ColumnType) bool {
	if ct == nil {
		return false
	}
	return databaseTypeNameLooksBinary(ct.DatabaseTypeName())
}

func databaseTypeNameLooksBinary(databaseTypeName string) bool {
	name := strings.ToUpper(databaseTypeName)
	if strings.Contains(name, "BLOB") || strings.Contains(name, "BYTEA") {
		return true
	}
	switch name {
	case "BINARY", "VARBINARY", "IMAGE", "RAW":
		return true
	}
	if strings.Contains(name, "VARBINARY") {
		return true
	}
	return false
}

// formatSQLValue converts scanned DB values into displayable strings.
func formatSQLValue(v interface{}) string {
	return formatSQLValueWithColumn(nil, v)
}

func formatSQLValueWithColumn(ct *sql.ColumnType, v interface{}) string {
	if v == nil {
		return "NULL"
	}
	switch x := v.(type) {
	case []byte:
		if sqlColumnLooksBinary(ct) {
			// Do not decode as PostgreSQL binary here — MySQL/MSSQL/SQLite wire formats differ
			// (e.g. MSSQL ROWVERSION/timestamp is 8-byte binary, not a PG timestamp).
			return formatByteaDisplay(x)
		}
		return string(x)
	case time.Time:
		// Preserve the driver's time value without applying PostgreSQL-specific layouts.
		return x.String()
	case net.IP:
		return x.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}
