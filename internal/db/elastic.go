package db

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/robertn/dbx/internal/config"
)

// elasticDriver implements Driver for Elasticsearch via the HTTP REST API.
// "Databases" map to a single default entry. "Tables" are ordinary indices plus data
// stream names (the names you query). Hidden .ds-* backing indices stay out of the tree.
// "Columns" are derived from the index or data stream mapping.
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

// Tables returns ordinary indices and data stream names. Backing indices (.ds-*) stay hidden.
// A data-stream listing failure still returns the indices so the tree is not empty.
func (d *elasticDriver) Tables(ctx context.Context, database string) ([]string, error) {
	entries, err := d.DetailedTables(ctx, database)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names, nil
}

// DetailedTables returns the same names as Tables. Detail is the on-disk size
// (store.size, every shard copy) and the live document count.
func (d *elasticDriver) DetailedTables(ctx context.Context, database string) ([]TableEntry, error) {
	listed, err := d.listIndices(ctx)
	if err != nil {
		return nil, err
	}
	streams, streamErr := d.listDataStreams(ctx)
	if streamErr != nil {
		streams = nil
	}
	for i := range streams {
		if n, ok := listed.streamDocs[streams[i].name]; ok {
			streams[i].docs = n
			streams[i].hasDocs = true
		}
	}
	items := mergeNamedSizes(listed.indices, streams)
	out := make([]TableEntry, len(items))
	for i, item := range items {
		out[i] = TableEntry{Name: item.name, Detail: formatTableDetail(item)}
	}
	return out, nil
}

func (d *elasticDriver) Views(ctx context.Context, database string) ([]string, error) {
	return []string{}, nil
}

func (d *elasticDriver) Columns(ctx context.Context, database, table string) ([]ColumnInfo, error) {
	return d.mappingColumns(ctx, table)
}

func (d *elasticDriver) AllTableColumns(ctx context.Context, database string) ([]TableColumn, error) {
	// expand_wildcards includes hidden backing indices (.ds-*) so data stream fields
	// can be attributed to the stream name. Other dot-prefixed system indices are skipped below.
	resp, err := d.doRequest(ctx, "GET", "/_mapping?expand_wildcards=open,hidden", nil)
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
	seen := make(map[string]struct{})
	for indexName, indexData := range raw {
		tableName := indexName
		if stream, ok := dataStreamNameFromBackingIndex(indexName); ok {
			tableName = stream
		} else if strings.HasPrefix(indexName, ".") {
			// Skip system indices. Data-stream backing indices are handled above.
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
			key := tableName + "\x00" + c.Name
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, TableColumn{Table: tableName, Name: c.Name, DataType: c.DataType})
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

// namedSize is an index or data stream plus its store size and live document count.
type namedSize struct {
	name    string
	bytes   int64
	hasSize bool
	docs    int64
	hasDocs bool
}

// indexList is the visible indices plus document counts summed from hidden
// data-stream backing indices (.ds-*).
type indexList struct {
	indices    []namedSize
	streamDocs map[string]int64
}

// listIndices returns non-system indices with size and document count.
// Hidden .ds-* backing indices are not listed; their docs.count is summed onto the stream name.
func (d *elasticDriver) listIndices(ctx context.Context) (indexList, error) {
	body, status, err := d.getBody(ctx, "/_cat/indices?h=index,store.size,docs.count&format=json&bytes=b&expand_wildcards=open,hidden")
	if err != nil {
		return indexList{}, err
	}
	if status >= 400 {
		return indexList{}, fmt.Errorf("failed to list indices (HTTP %d): %s", status, body)
	}
	return parseCatIndices(body)
}

// listDataStreams returns data stream names and their store size. These are the targets
// you query (for example logs-srds.usage.otel-default). Their documents live in hidden
// .ds-* backing indices, which _cat/indices omits and which listIndices skips.
// Names come from GET /_data_stream. Sizes come from /_data_stream/_stats and are omitted
// when that call fails, so the names still appear.
func (d *elasticDriver) listDataStreams(ctx context.Context) ([]namedSize, error) {
	body, status, err := d.getBody(ctx, "/_data_stream")
	if err != nil {
		return nil, err
	}
	// Older clusters have no data stream API.
	if status == http.StatusNotFound {
		return nil, nil
	}
	if status >= 400 {
		return nil, fmt.Errorf("failed to list data streams (HTTP %d): %s", status, body)
	}
	items, err := parseDataStreamNames(body)
	if err != nil {
		return nil, err
	}
	for name, n := range d.dataStreamSizes(ctx) {
		for i := range items {
			if items[i].name == name {
				items[i].bytes = n
				items[i].hasSize = true
				break
			}
		}
	}
	return items, nil
}

func parseDataStreamNames(body []byte) ([]namedSize, error) {
	var parsed struct {
		DataStreams []struct {
			Name string `json:"name"`
		} `json:"data_streams"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse data stream list: %v", err)
	}
	out := make([]namedSize, 0, len(parsed.DataStreams))
	for _, ds := range parsed.DataStreams {
		if ds.Name == "" || strings.HasPrefix(ds.Name, ".") {
			continue
		}
		out = append(out, namedSize{name: ds.Name})
	}
	return out, nil
}

// dataStreamSizes maps a data stream name to store_size_bytes. A failed stats call
// returns nil so the stream names can still be listed.
func (d *elasticDriver) dataStreamSizes(ctx context.Context) map[string]int64 {
	body, status, err := d.getBody(ctx, "/_data_stream/_stats")
	if err != nil || status >= 400 {
		return nil
	}
	var parsed struct {
		DataStreams []struct {
			Name  string      `json:"data_stream"`
			Bytes interface{} `json:"store_size_bytes"`
		} `json:"data_streams"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	out := make(map[string]int64, len(parsed.DataStreams))
	for _, ds := range parsed.DataStreams {
		if ds.Name == "" {
			continue
		}
		if n, ok := parseJSONInt(ds.Bytes); ok {
			out[ds.Name] = n
		}
	}
	return out
}

func (d *elasticDriver) getBody(ctx context.Context, path string) ([]byte, int, error) {
	resp, err := d.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func parseCatIndices(body []byte) (indexList, error) {
	var arr []map[string]interface{}
	if err := json.Unmarshal(body, &arr); err != nil {
		return indexList{}, fmt.Errorf("failed to parse index list: %v", err)
	}
	listed := indexList{streamDocs: make(map[string]int64)}
	for _, entry := range arr {
		name, _ := entry["index"].(string)
		if name == "" {
			continue
		}
		docs, hasDocs := parseJSONInt(entry["docs.count"])
		if stream, ok := dataStreamNameFromBackingIndex(name); ok {
			if hasDocs {
				listed.streamDocs[stream] += docs
			}
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue
		}
		item := namedSize{name: name}
		if n, ok := parseJSONInt(entry["store.size"]); ok {
			item.bytes = n
			item.hasSize = true
		}
		if hasDocs {
			item.docs = docs
			item.hasDocs = true
		}
		listed.indices = append(listed.indices, item)
	}
	return listed, nil
}

func parseJSONInt(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case string:
		n = strings.TrimSpace(n)
		if n == "" || n == "null" {
			return 0, false
		}
		if i, err := strconv.ParseInt(n, 10, 64); err == nil {
			return i, true
		}
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false
		}
		return int64(f), true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}

// formatByteSize renders a byte count as B, KB, MB, GB, TB, or PB (1024-based).
func formatByteSize(n int64) string {
	if n < 0 {
		return ""
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	val := float64(n)
	unit := 0
	for val >= 1024 && unit < len(units)-1 {
		val /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d B", n)
	}
	rounded := math.Round(val*10) / 10
	if rounded == math.Trunc(rounded) {
		return fmt.Sprintf("%.0f %s", rounded, units[unit])
	}
	return fmt.Sprintf("%.1f %s", rounded, units[unit])
}

// formatDocCount renders a live document count, for example "1 doc" or "12,345 docs".
func formatDocCount(n int64) string {
	if n < 0 {
		return ""
	}
	raw := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i := range raw {
		if i > 0 && (len(raw)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(raw[i])
	}
	if n == 1 {
		return b.String() + " doc"
	}
	return b.String() + " docs"
}

func formatTableDetail(item namedSize) string {
	var parts []string
	if item.hasSize {
		if size := formatByteSize(item.bytes); size != "" {
			parts = append(parts, size)
		}
	}
	if item.hasDocs {
		if docs := formatDocCount(item.docs); docs != "" {
			parts = append(parts, docs)
		}
	}
	return strings.Join(parts, ", ")
}

func mergeNamedSizes(groups ...[]namedSize) []namedSize {
	byName := make(map[string]namedSize)
	var names []string
	for _, group := range groups {
		for _, item := range group {
			if item.name == "" {
				continue
			}
			prev, ok := byName[item.name]
			if !ok {
				byName[item.name] = item
				names = append(names, item.name)
				continue
			}
			if !prev.hasSize && item.hasSize {
				prev.bytes = item.bytes
				prev.hasSize = true
			}
			if !prev.hasDocs && item.hasDocs {
				prev.docs = item.docs
				prev.hasDocs = true
			}
			byName[item.name] = prev
		}
	}
	sort.Strings(names)
	out := make([]namedSize, len(names))
	for i, name := range names {
		out[i] = byName[name]
	}
	return out
}

// backingIndexDateGen is the rollover suffix Elasticsearch appends to a data stream
// backing index: .ds-<stream>-<yyyy.MM.dd>-<generation>.
var backingIndexDateGen = regexp.MustCompile(`-\d{4}\.\d{2}\.\d{2}-\d+$`)

// dataStreamNameFromBackingIndex extracts the stream name from a hidden backing index.
// .ds-logs-srds.usage.otel-default-2026.11.11-000001 → logs-srds.usage.otel-default.
func dataStreamNameFromBackingIndex(index string) (string, bool) {
	if !strings.HasPrefix(index, ".ds-") {
		return "", false
	}
	rest := strings.TrimPrefix(index, ".ds-")
	loc := backingIndexDateGen.FindStringIndex(rest)
	if loc == nil || loc[0] == 0 {
		return "", false
	}
	return rest[:loc[0]], true
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
