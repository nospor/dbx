package db

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/robertn/dbx/internal/config"
	"gopkg.in/istreamdata/orientgo.v2"
	_ "gopkg.in/istreamdata/orientgo.v2/obinary" // register network protocol
)

type orientDriver struct {
	conn         config.Connection
	binaryClient *orient.Client
	binaryDB     *orient.Database
}

func (d *orientDriver) isBinary() bool {
	return strings.ToLower(d.conn.Protocol) == "binary"
}

func (d *orientDriver) Connect(ctx context.Context, conn config.Connection) error {
	d.conn = conn
	if d.conn.Port == 0 {
		if d.isBinary() {
			d.conn.Port = 2424
		} else {
			d.conn.Port = 2480
		}
	}

	if d.isBinary() {
		addr := fmt.Sprintf("%s:%d", d.conn.Host, d.conn.Port)
		var err error
		d.binaryClient, err = orient.Dial(addr)
		if err != nil {
			return fmt.Errorf("orientdb binary dial error: %v", err)
		}
		if d.conn.Database != "" {
			d.binaryDB, err = d.binaryClient.Open(d.conn.Database, orient.GraphDB, d.conn.User, d.conn.Password)
			if err != nil {
				return fmt.Errorf("orientdb binary open error: %v", err)
			}
		}
		return nil
	}

	if d.conn.Database == "" {
		// Just check if server is alive if no database specified
		url := fmt.Sprintf("http://%s:%d/listDatabases", d.conn.Host, d.conn.Port)
		resp, err := d.doRequest(ctx, "GET", url, nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("orientdb server returned status %d", resp.StatusCode)
		}
		return nil
	}
	url := fmt.Sprintf("http://%s:%d/connect/%s", d.conn.Host, d.conn.Port, url.PathEscape(d.conn.Database))
	resp, err := d.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("orientdb error (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func (d *orientDriver) Close() error {
	if d.binaryDB != nil {
		d.binaryDB.Close()
	}
	if d.binaryClient != nil {
		d.binaryClient.Close()
	}
	return nil
}

func (d *orientDriver) Ping(ctx context.Context) error {
	if d.isBinary() {
		if d.binaryClient == nil {
			return fmt.Errorf("orientdb binary client is nil")
		}
		admin, err := d.binaryClient.Auth(d.conn.User, d.conn.Password)
		if err != nil {
			return err
		}
		admin.Close()
		return nil
	}
	url := fmt.Sprintf("http://%s:%d/listDatabases", d.conn.Host, d.conn.Port)
	resp, err := d.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping failed: %d", resp.StatusCode)
	}
	return nil
}

func (d *orientDriver) Databases(ctx context.Context) ([]string, error) {
	if d.isBinary() {
		admin, err := d.binaryClient.Auth(d.conn.User, d.conn.Password)
		if err != nil {
			return nil, err
		}
		defer admin.Close()
		dbs, err := admin.ListDatabases()
		if err != nil {
			return nil, err
		}
		var names []string
		for name := range dbs {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	}
	url := fmt.Sprintf("http://%s:%d/listDatabases", d.conn.Host, d.conn.Port)
	resp, err := d.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Databases []string `json:"databases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Databases, nil
}

func (d *orientDriver) ensureDB(database string) error {
	if d.binaryDB != nil && d.binaryDB.GetCurDB() != nil && d.binaryDB.GetCurDB().Name == database {
		return nil
	}
	var err error
	d.binaryDB, err = d.binaryClient.Open(database, orient.GraphDB, d.conn.User, d.conn.Password)
	return err
}

func (d *orientDriver) Tables(ctx context.Context, database string) ([]string, error) {
	if d.isBinary() {
		if err := d.ensureDB(database); err != nil {
			return nil, err
		}
		cur := d.binaryDB.GetCurDB()
		if cur == nil {
			return nil, fmt.Errorf("failed to get current database metadata")
		}
		var tables []string
		for name := range cur.Classes {
			tables = append(tables, name)
		}
		sort.Strings(tables)
		return tables, nil
	}
	classes, err := d.fetchMetadata(ctx, database)
	if err != nil {
		return nil, err
	}
	var tables []string
	for _, c := range classes {
		tables = append(tables, c.Name)
	}
	sort.Strings(tables)
	return tables, nil
}

func (d *orientDriver) Views(ctx context.Context, database string) ([]string, error) {
	return nil, nil
}

func (d *orientDriver) Columns(ctx context.Context, database, table string) ([]ColumnInfo, error) {
	if d.isBinary() {
		if err := d.ensureDB(database); err != nil {
			return nil, err
		}
		cur := d.binaryDB.GetCurDB()
		if cur == nil {
			return nil, fmt.Errorf("failed to get current database metadata")
		}
		class, ok := cur.Classes[table]
		if !ok {
			return nil, fmt.Errorf("class %q not found", table)
		}
		var cols []ColumnInfo
		for name, p := range class.Properties {
			cols = append(cols, ColumnInfo{Name: name, DataType: fmt.Sprintf("%v", p.Type)})
		}
		sort.Slice(cols, func(i, j int) bool { return cols[i].Name < cols[j].Name })
		return cols, nil
	}
	classes, err := d.fetchMetadata(ctx, database)
	if err != nil {
		return nil, err
	}
	for _, c := range classes {
		if c.Name == table {
			var cols []ColumnInfo
			for _, p := range c.Properties {
				cols = append(cols, ColumnInfo{Name: p.Name, DataType: p.Type})
			}
			return cols, nil
		}
	}
	return nil, fmt.Errorf("class %q not found", table)
}

func (d *orientDriver) AllTableColumns(ctx context.Context, database string) ([]TableColumn, error) {
	if d.isBinary() {
		if err := d.ensureDB(database); err != nil {
			return nil, err
		}
		cur := d.binaryDB.GetCurDB()
		if cur == nil {
			return nil, fmt.Errorf("failed to get current database metadata")
		}
		var res []TableColumn
		for className, c := range cur.Classes {
			for propName, p := range c.Properties {
				res = append(res, TableColumn{Table: className, Name: propName, DataType: fmt.Sprintf("%v", p.Type)})
			}
		}
		return res, nil
	}
	classes, err := d.fetchMetadata(ctx, database)
	if err != nil {
		return nil, err
	}
	var res []TableColumn
	for _, c := range classes {
		for _, p := range c.Properties {
			res = append(res, TableColumn{Table: c.Name, Name: p.Name, DataType: p.Type})
		}
	}
	return res, nil
}

func (d *orientDriver) PrimaryKeyColumns(ctx context.Context, database, schema, table string) ([]string, error) {
	return []string{"@rid"}, nil
}

func (d *orientDriver) TableDDL(ctx context.Context, database, table string, isView bool) (string, error) {
	if d.isBinary() {
		if err := d.ensureDB(database); err != nil {
			return "", err
		}
		cur := d.binaryDB.GetCurDB()
		if cur == nil {
			return "", fmt.Errorf("failed to get current database metadata")
		}
		class, ok := cur.Classes[table]
		if !ok {
			return "", fmt.Errorf("class %q not found", table)
		}
		b, _ := json.MarshalIndent(class, "", "  ")
		return string(b), nil
	}
	classes, err := d.fetchMetadata(ctx, database)
	if err != nil {
		return "", err
	}
	for _, c := range classes {
		if c.Name == table {
			b, _ := json.MarshalIndent(c, "", "  ")
			return string(b), nil
		}
	}
	return "", fmt.Errorf("class %q not found", table)
}

func (d *orientDriver) Query(ctx context.Context, database, sql string) (*QueryResult, error) {
	if d.isBinary() {
		if err := d.ensureDB(database); err != nil {
			return nil, err
		}
		results := d.binaryDB.Command(orient.NewSQLCommand(sql))
		defer results.Close()
		if err := results.Err(); err != nil {
			return nil, err
		}
		return d.flattenBinaryResults(results), nil
	}
	url := fmt.Sprintf("http://%s:%d/command/%s/sql", d.conn.Host, d.conn.Port, url.PathEscape(database))
	body := map[string]interface{}{
		"command":  sql,
		"language": "sql",
	}
	resp, err := d.doRequest(ctx, "POST", url, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("orientdb error (%d): %s", resp.StatusCode, string(respBody))
	}

	var res struct {
		Result []map[string]interface{} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	return d.flattenResults(res.Result), nil
}

func (d *orientDriver) Exec(ctx context.Context, database, sql string) (*QueryResult, error) {
	return d.Query(ctx, database, sql)
}

func (d *orientDriver) flattenBinaryResults(res orient.Results) *QueryResult {
	var results interface{}
	if err := res.All(&results); err != nil {
		return &QueryResult{Error: err.Error()}
	}

	var maps []map[string]interface{}
	switch v := results.(type) {
	case []orient.OIdentifiable:
		for _, id := range v {
			if doc, ok := id.(*orient.Document); ok {
				maps = append(maps, d.documentToMap(doc))
			}
		}
	case *orient.Document:
		maps = append(maps, d.documentToMap(v))
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				maps = append(maps, m)
			} else if doc, ok := item.(*orient.Document); ok {
				maps = append(maps, d.documentToMap(doc))
			}
		}
	default:
		// Fallback for single values etc.
		maps = append(maps, map[string]interface{}{"Result": fmt.Sprintf("%v", v)})
	}

	return d.flattenResults(maps)
}

func (d *orientDriver) documentToMap(doc *orient.Document) map[string]interface{} {
	m := make(map[string]interface{})
	for name, entry := range doc.Fields() {
		m[name] = entry.Value
	}
	m["@rid"] = doc.GetIdentity().String()
	m["@class"] = doc.ClassName()
	m["@version"] = doc.Version()
	return m
}

type orientClass struct {
	Name       string           `json:"name"`
	Properties []orientProperty `json:"properties"`
}

type orientProperty struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func (d *orientDriver) fetchMetadata(ctx context.Context, database string) ([]orientClass, error) {
	url := fmt.Sprintf("http://%s:%d/database/%s", d.conn.Host, d.conn.Port, url.PathEscape(database))
	resp, err := d.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("orientdb error fetching metadata (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Classes []orientClass `json:"classes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode orientdb metadata: %v", err)
	}
	return result.Classes, nil
}

func (d *orientDriver) doRequest(ctx context.Context, method, url string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(d.conn.User, d.conn.Password)

	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}

func (d *orientDriver) flattenResults(docs []map[string]interface{}) *QueryResult {
	if len(docs) == 0 {
		return &QueryResult{
			Columns: []string{"Result"},
			Rows:    [][]string{{"OK (0 documents returned)"}},
		}
	}

	keySet := make(map[string]struct{})
	for _, doc := range docs {
		for k := range doc {
			keySet[k] = struct{}{}
		}
	}

	var metaCols []string
	var dataCols []string

	for k := range keySet {
		if strings.HasPrefix(k, "@") {
			metaCols = append(metaCols, k)
		} else {
			dataCols = append(dataCols, k)
		}
	}
	sort.Strings(metaCols)
	sort.Strings(dataCols)

	var finalCols []string
	special := []string{"@rid", "@class", "@version"}
	for _, s := range special {
		found := false
		for i, m := range metaCols {
			if m == s {
				finalCols = append(finalCols, m)
				metaCols = append(metaCols[:i], metaCols[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			for i, d := range dataCols {
				if d == s {
					finalCols = append(finalCols, d)
					dataCols = append(dataCols[:i], dataCols[i+1:]...)
					break
				}
			}
		}
	}
	finalCols = append(finalCols, metaCols...)
	finalCols = append(finalCols, dataCols...)

	var rows [][]string
	for _, doc := range docs {
		var row []string
		for _, col := range finalCols {
			if val, ok := doc[col]; ok {
				if val == nil {
					row = append(row, "null")
					continue
				}
				switch v := val.(type) {
				case string:
					row = append(row, v)
				case map[string]interface{}, []interface{}:
					b, _ := json.Marshal(v)
					row = append(row, string(b))
				default:
					row = append(row, fmt.Sprintf("%v", v))
				}
			} else {
				row = append(row, "")
			}
		}
		rows = append(rows, row)
	}

	return &QueryResult{
		Columns: finalCols,
		Rows:    rows,
	}
}
