package db

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestFormatByteSize(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{12345, "12.1 KB"},
		{891289600, "850 MB"},
		{1288490189, "1.2 GB"},
		{-1, ""},
	}
	for _, tc := range cases {
		if got := formatByteSize(tc.n); got != tc.want {
			t.Errorf("formatByteSize(%d) = %q; want %q", tc.n, got, tc.want)
		}
	}
}

func TestDataStreamNameFromBackingIndex(t *testing.T) {
	cases := []struct {
		index string
		name  string
		ok    bool
	}{
		{".ds-logs-srds.usage.otel-default-2026.11.11-000001", "logs-srds.usage.otel-default", true},
		{".ds-traces-srds.usage.otel-default-2026.11.11-000001", "traces-srds.usage.otel-default", true},
		{".ds-logs-foo-2020.01.01-2026.11.11-000007", "logs-foo-2020.01.01", true},
		{"srds-records", "", false},
		{".kibana", "", false},
		{".ds-missing-suffix", "", false},
	}
	for _, tc := range cases {
		name, ok := dataStreamNameFromBackingIndex(tc.index)
		if ok != tc.ok || name != tc.name {
			t.Errorf("dataStreamNameFromBackingIndex(%q) = %q, %v; want %q, %v", tc.index, name, ok, tc.name, tc.ok)
		}
	}
}

func TestElasticTablesIncludesDataStreams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_cat/indices":
			if r.URL.Query().Get("bytes") != "b" {
				t.Errorf("indices bytes = %q", r.URL.Query().Get("bytes"))
			}
			_, _ = w.Write([]byte(`[
				{"index":"srds-records","store.size":"891289600"},
				{"index":"srds-also-viewed","store.size":"12345"},
				{"index":".ds-logs-srds.usage.otel-default-2026.11.11-000001","store.size":"10"},
				{"index":".kibana","store.size":"10"}
			]`))
		case "/_data_stream":
			_, _ = w.Write([]byte(`{
				"data_streams": [
					{"name":"logs-srds.usage.otel-default"},
					{"name":"traces-srds.usage.otel-default"},
					{"name":".monitoring-es"}
				]
			}`))
		case "/_data_stream/_stats":
			_, _ = w.Write([]byte(`{
				"data_streams": [
					{"data_stream":"logs-srds.usage.otel-default","store_size_bytes":1288490189},
					{"data_stream":"traces-srds.usage.otel-default","store_size_bytes":0}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := &elasticDriver{baseURL: srv.URL, client: srv.Client()}
	got, err := d.Tables(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"logs-srds.usage.otel-default",
		"srds-also-viewed",
		"srds-records",
		"traces-srds.usage.otel-default",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tables() = %#v; want %#v", got, want)
	}

	detailed, err := d.DetailedTables(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	wantDetailed := []TableEntry{
		{Name: "logs-srds.usage.otel-default", Detail: "1.2 GB"},
		{Name: "srds-also-viewed", Detail: "12.1 KB"},
		{Name: "srds-records", Detail: "850 MB"},
		{Name: "traces-srds.usage.otel-default", Detail: "0 B"},
	}
	if !reflect.DeepEqual(detailed, wantDetailed) {
		t.Fatalf("DetailedTables() = %#v; want %#v", detailed, wantDetailed)
	}
}

func TestElasticDataStreamsListedWhenStatsFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_cat/indices":
			_, _ = w.Write([]byte(`[{"index":"srds-records","store.size":"1024"},{"index":"aaa-index","store.size":"2048"}]`))
		case "/_data_stream":
			_, _ = w.Write([]byte(`{"data_streams":[{"name":"logs-srds.usage.otel-default"}]}`))
		case "/_data_stream/_stats":
			http.Error(w, "no stats", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := &elasticDriver{baseURL: srv.URL, client: srv.Client()}
	got, err := d.DetailedTables(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	want := []TableEntry{
		{Name: "aaa-index", Detail: "2 KB"},
		{Name: "logs-srds.usage.otel-default"},
		{Name: "srds-records", Detail: "1 KB"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DetailedTables() = %#v; want %#v", got, want)
	}
}

func TestElasticTablesKeepsIndicesWhenDataStreamsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_data_stream" || r.URL.Path == "/_data_stream/_stats" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"index":"srds-records","store.size":"10"},{"index":"aaa-index","store.size":"20"},{"index":".kibana"}]`))
	}))
	defer srv.Close()

	d := &elasticDriver{baseURL: srv.URL, client: srv.Client()}
	got, err := d.Tables(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"aaa-index", "srds-records"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tables() = %#v; want %#v", got, want)
	}
}

func TestElasticAllTableColumnsAttributesBackingIndices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_mapping" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("expand_wildcards") != "open,hidden" {
			t.Errorf("expand_wildcards = %q", r.URL.Query().Get("expand_wildcards"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"srds-records": {"mappings": {"properties": {"title": {"type": "text"}}}},
			".kibana": {"mappings": {"properties": {"secret": {"type": "keyword"}}}},
			".ds-logs-srds.usage.otel-default-2026.11.11-000001": {
				"mappings": {"properties": {"message": {"type": "text"}, "@timestamp": {"type": "date"}}}
			},
			".ds-logs-srds.usage.otel-default-2026.11.11-000002": {
				"mappings": {"properties": {"message": {"type": "text"}, "trace_id": {"type": "keyword"}}}
			}
		}`))
	}))
	defer srv.Close()

	d := &elasticDriver{baseURL: srv.URL, client: srv.Client()}
	got, err := d.AllTableColumns(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}

	byTable := map[string]map[string]string{}
	for _, col := range got {
		if byTable[col.Table] == nil {
			byTable[col.Table] = map[string]string{}
		}
		if _, dup := byTable[col.Table][col.Name]; dup {
			t.Errorf("duplicate column %s.%s", col.Table, col.Name)
		}
		byTable[col.Table][col.Name] = col.DataType
	}
	if _, ok := byTable[".kibana"]; ok {
		t.Fatal("system index .kibana was included")
	}
	if _, ok := byTable[".ds-logs-srds.usage.otel-default-2026.11.11-000001"]; ok {
		t.Fatal("backing index was listed under its hidden name")
	}
	stream := byTable["logs-srds.usage.otel-default"]
	if stream["message"] == "" || stream["@timestamp"] == "" || stream["trace_id"] == "" || stream["_id"] == "" {
		t.Fatalf("stream columns = %#v", stream)
	}
	if byTable["srds-records"]["title"] != "text" {
		t.Fatalf("ordinary index columns = %#v", byTable["srds-records"])
	}
}
