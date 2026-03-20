package results

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// CloneQueryResult returns a deep copy suitable for caching (nil-safe).
func CloneQueryResult(r *QueryResult) *QueryResult {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Columns = append([]string(nil), r.Columns...)
	cp.Rows = make([][]string, len(r.Rows))
	for i, row := range r.Rows {
		cp.Rows[i] = append([]string(nil), row...)
	}
	return &cp
}

// ExportCSV writes the result to a CSV file. Returns the path written.
func (r *QueryResult) ExportCSV(dir string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("no results to export")
	}
	path := fmt.Sprintf("%s/dbx_export_%s.csv", dir, time.Now().Format("20060102_150405"))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write(r.Columns); err != nil {
		return "", err
	}
	for _, row := range r.Rows {
		if err := w.Write(row); err != nil {
			return "", err
		}
	}
	w.Flush()
	return path, w.Error()
}

// ExportJSON writes the result to a JSON file. Returns the path written.
func (r *QueryResult) ExportJSON(dir string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("no results to export")
	}
	path := fmt.Sprintf("%s/dbx_export_%s.json", dir, time.Now().Format("20060102_150405"))

	var records []map[string]string
	for _, row := range r.Rows {
		rec := make(map[string]string, len(r.Columns))
		for i, col := range r.Columns {
			if i < len(row) {
				rec[col] = row[i]
			}
		}
		records = append(records, rec)
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}
