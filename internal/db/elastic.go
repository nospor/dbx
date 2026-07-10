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

// elasticDriver implements Driver for Elasticsearch via the HTTP REST API.
// "Databases" map to indices, "Tables" to index names (same as database in this context),
// and "Columns" are derived from the index mapping.
//
// Query language: the user writes a JSON object that is sent as-is to the
// Elasticsearch Search API.  A few convenience shorthands are also supported:
//
//	GET /index/_search     → plain search body { "query": { ... }, "size": 100 }
//	PUT /index/_doc/id     → index a document
//	DELETE /index/_doc/id  → delete a document
//	POST /index/_update/id → update a document
//
// When the user types a bare JSON object (no leading verb) it is treated as
// an _search body against the currently active index.
type elasticDriver struct {
	baseURL string // e.g. http://localhost:9200
	client  *http.Client
	user    string
	pass    string
}

// ---------- lifecycle ----------

func (d *elasticDriver) Connect(ctx context.Context, conn config.Connection) error {
	scheme := "http"
	host := conn.Host
	if host == "" {
		host = "localhost"
	}
	// Allow the user to supply a full URL in the Host field.
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		d.baseURL = strings.TrimSuffix(host, "/")
	} else {
		port := conn.Port
		if port <= 0 {
			port = 9200
		}
		d.baseURL = fmt.Sprintf("%s://%s:%d", scheme, host, port)
	}
	d.user = conn.User
	d.pass = conn.Password
	d.client = &http.Client{Timeout: 30 * time.Second}
	return d.Ping(ctx)
}

func (d *elasticDriver) Close() error { return nil }

func (d *elasticDriver) Ping(ctx context.Context) error {
	resp, err := d.doRequest(ctx, "GET", "/", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("elasticsearch ping failed (HTTP %d): %s", resp.StatusCode, body)
	}
	return nil
}

// ---------- schema ----------

// Databases returns a single "default" database name since Elasticsearch has no databases.
func (d *elasticDriver) Databases(ctx context.Context) ([]string, error) {
	return []string{"default"}, nil
}

// Tables returns the list of all indices (non-system) as "tables".
func (d *elasticDriver) Tables(ctx context.Context, database string) ([]string, error) {
	return d.listIndices(ctx)
}

func (d *elasticDriver) Views(ctx context.Context, database string) ([]string, error) {
	return []string{}, nil
}

func (d *elasticDriver) Columns(ctx context.Context, database, table string) ([]ColumnInfo, error) {
	return d.mappingColumns(ctx, table)
}

func (d *elasticDriver) AllTableColumns(ctx context.Context, database string) ([]TableColumn, error) {
	resp, err := d.doRequest(ctx, "GET", "/_mapping", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("mapping request failed (HTTP %d): %s", resp.StatusCode, body)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse mapping: %v", err)
	}

	var out []TableColumn
	for indexName, indexData := range raw {
		// Skip system indices
		if strings.HasPrefix(indexName, ".") {
			continue
		}
		idxMap, ok := indexData.(map[string]interface{})
		if !ok {
			continue
		}
		mappings, ok := idxMap["mappings"].(map[string]interface{})
		if !ok {
			continue
		}

		var cols []ColumnInfo
		cols = append(cols, ColumnInfo{Name: "_id", DataType: "keyword"})

		if props, ok := mappings["properties"].(map[string]interface{}); ok {
			flattenMappingProperties("", props, &cols)
		}

		for _, c := range cols {
			out = append(out, TableColumn{Table: indexName, Name: c.Name, DataType: c.DataType})
		}
	}
	return out, nil
}

func (d *elasticDriver) PrimaryKeyColumns(ctx context.Context, database, schema, table string) ([]string, error) {
	return []string{"_id"}, nil
}

// TableDDL returns the mapping definition for the index as formatted JSON.
func (d *elasticDriver) TableDDL(ctx context.Context, database, table string, isView bool) (string, error) {
	index := table
	if index == "" {
		index = database
	}
	if index == "" || index == "default" {
		return "", fmt.Errorf("no index specified")
	}
	resp, err := d.doRequest(ctx, "GET", "/"+url.PathEscape(index)+"/_mapping", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var raw interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return string(body), nil
	}
	pretty, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return string(body), nil
	}
	return string(pretty), nil
}

// ---------- query ----------

// Exec delegates to Query (Elasticsearch has no separate DML path for us).
func (d *elasticDriver) Exec(ctx context.Context, database, sqlStr string) (*QueryResult, error) {
	return d.Query(ctx, database, sqlStr)
}

// Query executes an Elasticsearch request.
//
// Supported formats:
//
//  1. Shorthand verb lines:
//     GET  /path [body]
//     POST /path [body]
//     PUT  /path  body
//     DELETE /path
//     HEAD /path
//
//  2. Bare JSON object  → treated as _search body against `database` index.
func (d *elasticDriver) Query(ctx context.Context, database, sqlStr string) (*QueryResult, error) {
	if database == "" {
		return nil, fmt.Errorf("no index selected")
	}
	sqlTrimmed := strings.TrimSpace(sqlStr)
	if sqlTrimmed == "" {
		return nil, fmt.Errorf("empty query")
	}

	method, path, body, err := d.parseElasticCommand(sqlTrimmed, database)
	if err != nil {
		return nil, err
	}

	resp, err := d.doRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		// Try to surface the ES error message nicely.
		var errDoc map[string]interface{}
		if json.Unmarshal(respBody, &errDoc) == nil {
			if em, ok := errDoc["error"]; ok {
				b, _ := json.MarshalIndent(em, "", "  ")
				return nil, fmt.Errorf("elasticsearch error (HTTP %d):\n%s", resp.StatusCode, b)
			}
		}
		return nil, fmt.Errorf("elasticsearch error (HTTP %d): %s", resp.StatusCode, respBody)
	}

	return d.parseResponse(respBody)
}

// ---------- internal helpers ----------

func (d *elasticDriver) doRequest(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	u := d.baseURL + path
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if d.user != "" {
		req.SetBasicAuth(d.user, d.pass)
	}
	return d.client.Do(req)
}

// parseElasticCommand parses the query text into (method, path, body).
//
// Supported shorthand:
//
//	GET /path
//	POST /path { ... }
//	PUT /path { ... }
//	DELETE /path
//	HEAD /path
//	{ ... }  →  POST /<database>/_search  { ... }
//
// Also accepts ndjson bulk format when the first line is a JSON object with
// {"index":...}, {"delete":...}, etc.
func (d *elasticDriver) parseElasticCommand(text, database string) (method, path string, body []byte, err error) {
	upper := strings.ToUpper(text)

	// Shorthand verb
	for _, verb := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD"} {
		if strings.HasPrefix(upper, verb+" ") || strings.HasPrefix(upper, verb+"\t") {
			rest := strings.TrimSpace(text[len(verb):])
			// Split into path + optional JSON body
			spaceIdx := -1
			for i, r := range rest {
				if r == ' ' || r == '\t' || r == '\n' {
					spaceIdx = i
					break
				}
			}
			if spaceIdx < 0 {
				// No body
				return verb, rest, nil, nil
			}
			p := rest[:spaceIdx]
			bodyStr := strings.TrimSpace(rest[spaceIdx:])
			if bodyStr == "" {
				return verb, p, nil, nil
			}
			return verb, p, []byte(bodyStr), nil
		}
	}

	// Bare JSON → search body
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		path = "/" + url.PathEscape(database) + "/_search"
		if database == "default" {
			path = "/_search"
		}
		return "POST", path, []byte(text), nil
	}

	return "", "", nil, fmt.Errorf("unsupported Elasticsearch command format.\n" +
		"Use: GET /index/_search, POST /index/_search {...}, or a bare JSON search body.\n" +
		"Example: {\"query\":{\"match_all\":{}},\"size\":100}")
}

// parseResponse turns the raw Elasticsearch JSON response into a QueryResult.
func (d *elasticDriver) parseResponse(data []byte) (*QueryResult, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		// Not JSON — return as single cell.
		return &QueryResult{
			Columns: []string{"response"},
			Rows:    [][]string{{string(data)}},
		}, nil
	}

	// _search / _msearch response: unwrap hits.hits
	if hitsOuter, ok := raw["hits"].(map[string]interface{}); ok {
		if hitsArr, ok := hitsOuter["hits"].([]interface{}); ok {
			return d.hitsToResult(hitsArr)
		}
	}

	// Bulk / index / delete / update acknowledgement
	if _, hasAck := raw["acknowledged"]; hasAck {
		pretty, _ := json.MarshalIndent(raw, "", "  ")
		return &QueryResult{
			Columns: []string{"result"},
			Rows:    [][]string{{string(pretty)}},
		}, nil
	}

	// _cat responses or single-document GET (_source)
	if src, ok := raw["_source"]; ok {
		if srcMap, ok := src.(map[string]interface{}); ok {
			id := fmt.Sprintf("%v", raw["_id"])
			srcMap["_id"] = id
			return d.docsToResult([]map[string]interface{}{srcMap})
		}
	}

	// Fallback: render as pretty JSON
	pretty, _ := json.MarshalIndent(raw, "", "  ")
	return &QueryResult{
		Columns: []string{"response"},
		Rows:    [][]string{{string(pretty)}},
	}, nil
}

// hitsToResult converts an ES hits array to a QueryResult table.
func (d *elasticDriver) hitsToResult(hits []interface{}) (*QueryResult, error) {
	if len(hits) == 0 {
		return &QueryResult{
			Columns: []string{"result"},
			Rows:    [][]string{{"OK (0 hits)"}},
		}, nil
	}

	var docs []map[string]interface{}
	for _, h := range hits {
		hit, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		doc := make(map[string]interface{})
		// Always include metadata
		doc["_id"] = fmt.Sprintf("%v", hit["_id"])
		if idx, ok := hit["_index"]; ok {
			doc["_index"] = fmt.Sprintf("%v", idx)
		}
		// Merge _source fields
		if src, ok := hit["_source"].(map[string]interface{}); ok {
			for k, v := range src {
				doc[k] = v
			}
		}
		// Merge fields (runtime fields / script_fields)
		if fields, ok := hit["fields"].(map[string]interface{}); ok {
			for k, v := range fields {
				doc[k] = v
			}
		}
		docs = append(docs, doc)
	}
	return d.docsToResult(docs)
}

// docsToResult converts a slice of flat document maps to a QueryResult.
func (d *elasticDriver) docsToResult(docs []map[string]interface{}) (*QueryResult, error) {
	if len(docs) == 0 {
		return &QueryResult{
			Columns: []string{"result"},
			Rows:    [][]string{{"OK (0 documents)"}},
		}, nil
	}

	// Collect all distinct keys
	keySet := make(map[string]struct{})
	for _, doc := range docs {
		for k := range doc {
			keySet[k] = struct{}{}
		}
	}
	var columns []string
	hasID := false
	hasIndex := false
	for k := range keySet {
		switch k {
		case "_id":
			hasID = true
		case "_index":
			hasIndex = true
		default:
			columns = append(columns, k)
		}
	}
	sort.Strings(columns)
	if hasIndex {
		columns = append([]string{"_index"}, columns...)
	}
	if hasID {
		columns = append([]string{"_id"}, columns...)
	}

	var rows [][]string
	for _, doc := range docs {
		var row []string
		for _, col := range columns {
			val, ok := doc[col]
			if !ok {
				row = append(row, "")
				continue
			}
			row = append(row, formatElasticValue(val))
		}
		rows = append(rows, row)
	}
	return &QueryResult{Columns: columns, Rows: rows}, nil
}

func formatElasticValue(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		// Avoid scientific notation for large integers
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case map[string]interface{}, []interface{}:
		b, _ := json.Marshal(v)
		return string(b)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// listIndices returns all non-system index names.
func (d *elasticDriver) listIndices(ctx context.Context) ([]string, error) {
	resp, err := d.doRequest(ctx, "GET", "/_cat/indices?h=index&format=json", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("failed to list indices (HTTP %d): %s", resp.StatusCode, body)
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, fmt.Errorf("failed to parse index list: %v", err)
	}

	var names []string
	for _, entry := range arr {
		name, ok := entry["index"].(string)
		if !ok {
			continue
		}
		// Skip system indices
		if strings.HasPrefix(name, ".") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// mappingColumns extracts field names + types from the index mapping.
func (d *elasticDriver) mappingColumns(ctx context.Context, index string) ([]ColumnInfo, error) {
	if index == "" {
		return nil, fmt.Errorf("no index specified")
	}
	resp, err := d.doRequest(ctx, "GET", "/"+url.PathEscape(index)+"/_mapping", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("mapping request failed (HTTP %d): %s", resp.StatusCode, body)
	}

	// Response shape: { "<index>": { "mappings": { "properties": { ... } } } }
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse mapping: %v", err)
	}

	var cols []ColumnInfo
	// Always include the meta fields.
	cols = append(cols, ColumnInfo{Name: "_id", DataType: "keyword"})

	for _, indexData := range raw {
		idxMap, ok := indexData.(map[string]interface{})
		if !ok {
			continue
		}
		mappings, ok := idxMap["mappings"].(map[string]interface{})
		if !ok {
			continue
		}
		props, ok := mappings["properties"].(map[string]interface{})
		if !ok {
			continue
		}
		flattenMappingProperties("", props, &cols)
		break // only first (there's only one)
	}

	return cols, nil
}

// flattenMappingProperties recursively walks the ES mapping properties tree
// and appends ColumnInfo for each leaf field (using dot-notation for nested fields).
func flattenMappingProperties(prefix string, props map[string]interface{}, cols *[]ColumnInfo) {
	// Collect and sort keys for deterministic order
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		fieldDef, ok := props[name].(map[string]interface{})
		if !ok {
			continue
		}
		fullName := name
		if prefix != "" {
			fullName = prefix + "." + name
		}
		dataType := "object"
		if t, ok := fieldDef["type"].(string); ok {
			dataType = t
		}
		*cols = append(*cols, ColumnInfo{Name: fullName, DataType: dataType})
		// Recurse into nested / object fields
		if nested, ok := fieldDef["properties"].(map[string]interface{}); ok {
			flattenMappingProperties(fullName, nested, cols)
		}
	}
}
