package db

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/robertn/dbx/internal/config"
)

type dbfTestField struct {
	name     string
	typ      byte
	length   uint8
	decimals uint8
}

func writeDBaseIII(t *testing.T, path string, fields []dbfTestField, records [][]string, deleted []bool) {
	t.Helper()
	if len(deleted) != len(records) {
		t.Fatalf("deleted flags (%d) must match record count (%d)", len(deleted), len(records))
	}
	headerLen := uint16(32 + 32*len(fields) + 1)
	recLen := uint16(1)
	for _, f := range fields {
		recLen += uint16(f.length)
	}

	var hdr [32]byte
	hdr[0] = 0x03
	hdr[1] = 26
	hdr[2] = 1
	hdr[3] = 1
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(len(records)))
	binary.LittleEndian.PutUint16(hdr[8:10], headerLen)
	binary.LittleEndian.PutUint16(hdr[10:12], recLen)

	buf := append([]byte{}, hdr[:]...)
	for _, f := range fields {
		var desc [32]byte
		copy(desc[:11], f.name)
		desc[11] = f.typ
		desc[16] = f.length
		desc[17] = f.decimals
		buf = append(buf, desc[:]...)
	}
	buf = append(buf, 0x0D)

	for i, rec := range records {
		if deleted[i] {
			buf = append(buf, '*')
		} else {
			buf = append(buf, ' ')
		}
		for j, f := range fields {
			val := ""
			if j < len(rec) {
				val = rec[j]
			}
			field := make([]byte, f.length)
			for k := range field {
				field[k] = ' '
			}
			copy(field, val)
			buf = append(buf, field...)
		}
	}
	buf = append(buf, 0x1A)
	if err := os.WriteFile(path, buf, 0644); err != nil {
		t.Fatalf("write dbf: %v", err)
	}
}

func TestDBFConnectMissingPath(t *testing.T) {
	d := &dbfDriver{}
	err := d.Connect(context.Background(), config.Connection{Driver: "dbf"})
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestDBFConnectMissingFile(t *testing.T) {
	d := &dbfDriver{}
	err := d.Connect(context.Background(), config.Connection{
		Driver:   "dbf",
		FilePath: filepath.Join(t.TempDir(), "nope.dbf"),
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestDBFDirectoryAndFile(t *testing.T) {
	dir := t.TempDir()
	people := filepath.Join(dir, "people.dbf")
	orders := filepath.Join(dir, "ORDERS.DBF")
	writeDBaseIII(t, people, []dbfTestField{
		{name: "NAME", typ: 'C', length: 10},
		{name: "AGE", typ: 'N', length: 3},
	}, [][]string{
		{"Alice", "30"},
		{"Bob", "41"},
		{"Zed", "99"},
	}, []bool{false, true, false})
	writeDBaseIII(t, orders, []dbfTestField{
		{name: "ID", typ: 'C', length: 4},
	}, [][]string{{"A1"}}, []bool{false})

	ctx := context.Background()

	t.Run("directory lists tables", func(t *testing.T) {
		d := &dbfDriver{}
		if err := d.Connect(ctx, config.Connection{Driver: "dbf", FilePath: dir}); err != nil {
			t.Fatalf("connect: %v", err)
		}
		dbs, err := d.Databases(ctx)
		if err != nil {
			t.Fatalf("databases: %v", err)
		}
		if len(dbs) != 1 || dbs[0] != filepath.Base(dir) {
			t.Fatalf("databases = %v", dbs)
		}
		tables, err := d.Tables(ctx, "")
		if err != nil {
			t.Fatalf("tables: %v", err)
		}
		if len(tables) != 2 || tables[0] != "ORDERS" || tables[1] != "people" {
			t.Fatalf("tables = %v", tables)
		}
		cols, err := d.Columns(ctx, "", "people")
		if err != nil {
			t.Fatalf("columns: %v", err)
		}
		if len(cols) != 2 || cols[0].Name != "NAME" || cols[1].Name != "AGE" {
			t.Fatalf("columns = %+v", cols)
		}
	})

	t.Run("single file is one table", func(t *testing.T) {
		d := &dbfDriver{}
		if err := d.Connect(ctx, config.Connection{Driver: "dbf", FilePath: people}); err != nil {
			t.Fatalf("connect: %v", err)
		}
		tables, err := d.Tables(ctx, "")
		if err != nil {
			t.Fatalf("tables: %v", err)
		}
		if len(tables) != 1 || tables[0] != "people" {
			t.Fatalf("tables = %v", tables)
		}
	})

	t.Run("browse skips deleted and respects limit", func(t *testing.T) {
		d := &dbfDriver{}
		if err := d.Connect(ctx, config.Connection{Driver: "dbf", FilePath: dir}); err != nil {
			t.Fatalf("connect: %v", err)
		}
		res, err := d.Query(ctx, "", `{"browse":"people","limit":10}`)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if res.Error != "" {
			t.Fatalf("query error: %s", res.Error)
		}
		if len(res.Rows) != 2 {
			t.Fatalf("want 2 live rows, got %d: %v", len(res.Rows), res.Rows)
		}
		if res.Rows[0][0] != "Alice" || res.Rows[1][0] != "Zed" {
			t.Fatalf("rows = %v", res.Rows)
		}
		limited, err := d.Query(ctx, "", `{"browse":"people","limit":1}`)
		if err != nil {
			t.Fatalf("limited query: %v", err)
		}
		if limited.Error != "" {
			t.Fatalf("limited query error: %s", limited.Error)
		}
		if len(limited.Rows) != 1 || limited.Rows[0][0] != "Alice" {
			t.Fatalf("limited rows = %v", limited.Rows)
		}
	})

	t.Run("sql is rejected", func(t *testing.T) {
		d := &dbfDriver{}
		if err := d.Connect(ctx, config.Connection{Driver: "dbf", FilePath: dir}); err != nil {
			t.Fatalf("connect: %v", err)
		}
		res, err := d.Query(ctx, "", "SELECT * FROM people")
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if res.Error != dbfQueriesUnsupported {
			t.Fatalf("error = %q", res.Error)
		}
	})
}

func TestParseDBFBrowse(t *testing.T) {
	cmd, err := parseDBFBrowse(`{"browse":"customers"}`)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Browse != "customers" || cmd.Limit != dbfDefaultBrowseLimit {
		t.Fatalf("cmd = %+v", cmd)
	}
	if _, err := parseDBFBrowse("SELECT 1"); err == nil {
		t.Fatal("expected error")
	}
}
