package db

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/robertn/dbx/internal/config"
	"github.com/valentin-kaiser/go-dbase/dbase"
)

const (
	dbfDefaultBrowseLimit = 100
	dbfMaxBrowseLimit     = 10000
	dbfQueriesUnsupported = "dbf: queries are not supported yet; press s on a table in the explorer to browse rows"
)

type dbfDriver struct {
	path   string
	isFile bool
	name   string
}

type dbfTableFile struct {
	name string
	path string
}

type dbfBrowseCmd struct {
	Browse string `json:"browse"`
	Limit  int    `json:"limit"`
}

func (d *dbfDriver) Connect(_ context.Context, conn config.Connection) error {
	path := conn.FilePath
	if path == "" {
		path = conn.Database
	}
	if path == "" {
		return fmt.Errorf("dbf: file path is required (set file_path to a .dbf file or a directory)")
	}
	path = expandTilde(path)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("dbf: path %q does not exist", path)
		}
		return fmt.Errorf("dbf: stat %q: %w", path, err)
	}
	if info.IsDir() {
		d.path = path
		d.isFile = false
		d.name = catalogName(path, false)
		return nil
	}
	if !isDBFExt(path) {
		return fmt.Errorf("dbf: %q is not a .dbf file or directory", path)
	}
	d.path = path
	d.isFile = true
	d.name = catalogName(path, true)
	return nil
}

func (d *dbfDriver) Close() error { return nil }

func (d *dbfDriver) Ping(_ context.Context) error {
	if d.path == "" {
		return fmt.Errorf("dbf: not connected")
	}
	_, err := os.Stat(d.path)
	if err != nil {
		return fmt.Errorf("dbf: ping: %w", err)
	}
	return nil
}

func (d *dbfDriver) Databases(_ context.Context) ([]string, error) {
	if d.name == "" {
		return []string{"dbf"}, nil
	}
	return []string{d.name}, nil
}

func (d *dbfDriver) Tables(_ context.Context, _ string) ([]string, error) {
	files, err := d.listTables()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.name)
	}
	return names, nil
}

func (d *dbfDriver) Views(_ context.Context, _ string) ([]string, error) {
	return []string{}, nil
}

func (d *dbfDriver) Columns(ctx context.Context, _, table string) ([]ColumnInfo, error) {
	file, err := d.openTable(table)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var cols []ColumnInfo
	for _, c := range file.Columns() {
		if c == nil || isHiddenDBFColumn(c.Name()) {
			continue
		}
		cols = append(cols, ColumnInfo{Name: c.Name(), DataType: dbfDataType(c)})
	}
	return cols, nil
}

func (d *dbfDriver) AllTableColumns(ctx context.Context, database string) ([]TableColumn, error) {
	tables, err := d.Tables(ctx, database)
	if err != nil {
		return nil, err
	}
	var out []TableColumn
	for _, name := range tables {
		cols, err := d.Columns(ctx, database, name)
		if err != nil {
			continue
		}
		for _, c := range cols {
			out = append(out, TableColumn{Table: name, Name: c.Name, DataType: c.DataType})
		}
	}
	return out, nil
}

func (d *dbfDriver) PrimaryKeyColumns(context.Context, string, string, string) ([]string, error) {
	return []string{}, nil
}

func (d *dbfDriver) TableDDL(ctx context.Context, database, table string, isView bool) (string, error) {
	if isView {
		return "", fmt.Errorf("dbf: views are not supported")
	}
	cols, err := d.Columns(ctx, database, table)
	if err != nil {
		return "", err
	}
	tf, err := d.resolveTable(table)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "-- dbf table %s\n-- source: %s\nCREATE TABLE %s (\n", table, tf.path, table)
	for i, c := range cols {
		fmt.Fprintf(&b, "  %s %s", c.Name, c.DataType)
		if i < len(cols)-1 {
			b.WriteString(",")
		}
		b.WriteByte('\n')
	}
	b.WriteString(");")
	return b.String(), nil
}

func (d *dbfDriver) Query(ctx context.Context, _ string, sqlStr string) (*QueryResult, error) {
	cmd, err := parseDBFBrowse(sqlStr)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	return d.browseTable(ctx, cmd.Browse, cmd.Limit)
}

func (d *dbfDriver) Exec(ctx context.Context, database, sqlStr string) (*QueryResult, error) {
	return d.Query(ctx, database, sqlStr)
}

func (d *dbfDriver) browseTable(ctx context.Context, table string, limit int) (*QueryResult, error) {
	file, err := d.openTable(table)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	defer file.Close()

	var columns []string
	var colIndex []int
	for i, c := range file.Columns() {
		if c == nil || isHiddenDBFColumn(c.Name()) {
			continue
		}
		columns = append(columns, c.Name())
		colIndex = append(colIndex, i)
	}

	rows := make([][]string, 0, limit)
	for !file.EOF() {
		if err := ctx.Err(); err != nil {
			return &QueryResult{Error: err.Error()}, nil
		}
		row, err := file.Next()
		if err != nil {
			return &QueryResult{Error: err.Error()}, nil
		}
		if row.Deleted {
			continue
		}
		vals := row.Values()
		line := make([]string, len(colIndex))
		for j, idx := range colIndex {
			var v interface{}
			if idx < len(vals) {
				v = vals[idx]
			}
			line[j] = formatDBFValue(v)
		}
		rows = append(rows, line)
		if len(rows) >= limit {
			break
		}
	}
	return &QueryResult{Columns: columns, Rows: rows}, nil
}

func (d *dbfDriver) openTable(table string) (*dbase.File, error) {
	tf, err := d.resolveTable(table)
	if err != nil {
		return nil, err
	}
	file, err := dbase.OpenTable(&dbase.Config{
		Filename:                          tf.path,
		TrimSpaces:                        true,
		ReadOnly:                          true,
		Untested:                          true,
		InterpretCodePage:                 true,
		DisableConvertFilenameUnderscores: true,
	})
	if err != nil {
		return nil, fmt.Errorf("dbf: open %q: %w", tf.path, err)
	}
	return file, nil
}

func (d *dbfDriver) resolveTable(table string) (dbfTableFile, error) {
	files, err := d.listTables()
	if err != nil {
		return dbfTableFile{}, err
	}
	var ciMatch *dbfTableFile
	for i := range files {
		if files[i].name == table {
			return files[i], nil
		}
		if ciMatch == nil && strings.EqualFold(files[i].name, table) {
			ciMatch = &files[i]
		}
	}
	if ciMatch != nil {
		return *ciMatch, nil
	}
	return dbfTableFile{}, fmt.Errorf("dbf: table %q not found", table)
}

func (d *dbfDriver) listTables() ([]dbfTableFile, error) {
	if d.path == "" {
		return nil, fmt.Errorf("dbf: not connected")
	}
	if d.isFile {
		return []dbfTableFile{{name: tableNameFromPath(d.path), path: d.path}}, nil
	}
	entries, err := os.ReadDir(d.path)
	if err != nil {
		return nil, fmt.Errorf("dbf: read directory %q: %w", d.path, err)
	}
	var files []dbfTableFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isDBFExt(name) {
			continue
		}
		files = append(files, dbfTableFile{
			name: tableNameFromPath(name),
			path: filepath.Join(d.path, name),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].name) < strings.ToLower(files[j].name)
	})
	return files, nil
}

func parseDBFBrowse(sqlStr string) (dbfBrowseCmd, error) {
	trimmed := strings.TrimSpace(sqlStr)
	if trimmed == "" {
		return dbfBrowseCmd{}, fmt.Errorf("%s", dbfQueriesUnsupported)
	}
	var cmd dbfBrowseCmd
	if err := json.Unmarshal([]byte(trimmed), &cmd); err != nil || strings.TrimSpace(cmd.Browse) == "" {
		return dbfBrowseCmd{}, fmt.Errorf("%s", dbfQueriesUnsupported)
	}
	if cmd.Limit <= 0 {
		cmd.Limit = dbfDefaultBrowseLimit
	}
	if cmd.Limit > dbfMaxBrowseLimit {
		cmd.Limit = dbfMaxBrowseLimit
	}
	return cmd, nil
}

func catalogName(path string, isFile bool) string {
	p := path
	if isFile {
		p = filepath.Dir(path)
	}
	name := filepath.Base(p)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "dbf"
	}
	return name
}

func tableNameFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func isDBFExt(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".dbf")
}

func isHiddenDBFColumn(name string) bool {
	return strings.EqualFold(name, "_NullFlags")
}

func dbfTypeName(code string) string {
	switch strings.ToUpper(code) {
	case "C":
		return "Character"
	case "Y":
		return "Currency"
	case "B":
		return "Double"
	case "D":
		return "Date"
	case "T":
		return "DateTime"
	case "F":
		return "Float"
	case "I":
		return "Integer"
	case "L":
		return "Logical"
	case "M":
		return "Memo"
	case "N":
		return "Numeric"
	case "W":
		return "Blob"
	case "G":
		return "General"
	case "P":
		return "Picture"
	case "Q":
		return "Varbinary"
	case "V":
		return "Varchar"
	default:
		if code == "" {
			return "Unknown"
		}
		return code
	}
}

func dbfDataType(col *dbase.Column) string {
	name := dbfTypeName(col.Type())
	switch strings.ToUpper(col.Type()) {
	case "C", "V", "Q":
		return fmt.Sprintf("%s(%d)", name, col.Length)
	case "N", "F":
		if col.Decimals > 0 {
			return fmt.Sprintf("%s(%d,%d)", name, col.Length, col.Decimals)
		}
		return fmt.Sprintf("%s(%d)", name, col.Length)
	default:
		return name
	}
}

func formatDBFValue(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		if utf8.Valid(x) {
			return string(x)
		}
		return formatByteaDisplay(x)
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 && x.Nanosecond() == 0 {
			return x.Format("2006-01-02")
		}
		return x.Format(time.RFC3339)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float32:
		return fmt.Sprintf("%g", x)
	case float64:
		return fmt.Sprintf("%g", x)
	default:
		return fmt.Sprintf("%v", x)
	}
}
