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
)

type orientDriver struct {
	conn config.Connection
}

func (d *orientDriver) Connect(ctx context.Context, conn config.Connection) error {
	d.conn = conn
	if d.conn.Port == 0 {
		d.conn.Port = 2480
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
	return nil
}

func (d *orientDriver) Ping(ctx context.Context) error {
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

func (d *orientDriver) Tables(ctx context.Context, database string) ([]string, error) {
	classes, err := d.fetchMetadata(ctx, database)
	if err != nil {
		return nil, err
	}
	var tables []string
	for _, c := range classes {
		// Filter out internal classes if desired, but OrientDB often uses them.
		// For now, let's show all.
		tables = append(tables, c.Name)
	}
	sort.Strings(tables)
	return tables, nil
}

func (d *orientDriver) Views(ctx context.Context, database string) ([]string, error) {
	return nil, nil // OrientDB doesn't have "views" in the SQL sense in the classes list
}

func (d *orientDriver) Columns(ctx context.Context, database, table string) ([]ColumnInfo, error) {
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
	// OrientDB uses @rid as primary key
	return []string{"@rid"}, nil
}

func (d *orientDriver) TableDDL(ctx context.Context, database, table string, isView bool) (string, error) {
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
	// OrientDB query endpoint: POST /query/{db}/sql/{query}/{limit}
	// But /command is often more flexible. Let's try /command first as it supports everything.
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

	// OrientDB metadata fields usually start with @
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

	// Put @rid and @class first if they exist
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
			// check dataCols just in case
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
