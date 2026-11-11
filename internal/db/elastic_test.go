package db

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

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
			_, _ = w.Write([]byte(`[
				{"index":"srds-records"},
				{"index":"srds-also-viewed"},
				{"index":".ds-logs-srds.usage.otel-default-2026.11.11-000001"},
				{"index":".kibana"}
			]`))
		case "/_data_stream":
			_, _ = w.Write([]byte(`{
				"data_streams": [
					{"name":"logs-srds.usage.otel-default"},
					{"name":"traces-srds.usage.otel-default"},
					{"name":".monitoring-es"}
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
}

func TestElasticTablesKeepsIndicesWhenDataStreamsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_data_stream" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"index":"srds-records"},{"index":".kibana"}]`))
	}))
	defer srv.Close()

	d := &elasticDriver{baseURL: srv.URL, client: srv.Client()}
	got, err := d.Tables(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"srds-records"}
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
