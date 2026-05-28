package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/robertn/dbx/internal/db"
	"github.com/robertn/dbx/internal/ui/results"
	"github.com/robertn/dbx/internal/ui/theme"
)

type mockDriver struct {
	db.Driver
	queries []string
	execs   []string
	queryFn func(ctx context.Context, database, sql string) (*db.QueryResult, error)
	execFn  func(ctx context.Context, database, sql string) (*db.QueryResult, error)
}

func (m *mockDriver) Query(ctx context.Context, database, sql string) (*db.QueryResult, error) {
	m.queries = append(m.queries, sql)
	if m.queryFn != nil {
		return m.queryFn(ctx, database, sql)
	}
	return &db.QueryResult{Columns: []string{"col"}, Rows: [][]string{{"val"}}}, nil
}

func (m *mockDriver) Exec(ctx context.Context, database, sql string) (*db.QueryResult, error) {
	m.execs = append(m.execs, sql)
	if m.execFn != nil {
		return m.execFn(ctx, database, sql)
	}
	return &db.QueryResult{}, nil
}

func (m *mockDriver) Ping(ctx context.Context) error {
	return nil
}

func (m *mockDriver) Close() error {
	return nil
}

func TestExecMultiStatementBatch(t *testing.T) {
	drv := &mockDriver{}
	ctx := context.Background()
	stmts := []string{
		"SET @list_id = 1",
		"SELECT * FROM listing_tb WHERE list_id = @list_id",
	}

	res := execMultiStatementBatch(ctx, drv, "conn1", "db1", "tab1", stmts, time.Now(), "SET @list_id = 1;\nSELECT * FROM listing_tb WHERE list_id = @list_id;")

	if res.result.Error != "" {
		t.Fatalf("expected no error, got %s", res.result.Error)
	}

	if len(drv.execs) != 1 || drv.execs[0] != "SET @list_id = 1" {
		t.Fatalf("expected 1 exec with SET, got: %v", drv.execs)
	}

	if len(drv.queries) != 1 || drv.queries[0] != "SELECT * FROM listing_tb WHERE list_id = @list_id" {
		t.Fatalf("expected 1 query with SELECT, got: %v", drv.queries)
	}

	// Verify it returns the last statement's result
	if len(res.result.Columns) != 1 || res.result.Columns[0] != "col" || res.result.Rows[0][0] != "val" {
		t.Fatalf("expected SELECT results, got columns: %v rows: %v", res.result.Columns, res.result.Rows)
	}
}

func TestExecMultiStatementBatch_Error(t *testing.T) {
	drv := &mockDriver{
		execFn: func(ctx context.Context, database, sql string) (*db.QueryResult, error) {
			return nil, errors.New("exec error")
		},
	}
	ctx := context.Background()
	stmts := []string{
		"SET @list_id = 1",
		"SELECT * FROM listing_tb WHERE list_id = @list_id",
	}

	res := execMultiStatementBatch(ctx, drv, "conn1", "db1", "tab1", stmts, time.Now(), "SET @list_id = 1;\nSELECT * FROM listing_tb WHERE list_id = @list_id;")

	if res.result.Error == "" {
		t.Fatal("expected error, got none")
	}

	// It should stop at the first error, so no queries should be run
	if len(drv.queries) != 0 {
		t.Fatalf("expected no queries to run after exec error, got %d queries", len(drv.queries))
	}
}

func TestGetOrCreateDriverCaching(t *testing.T) {
	m := &Model{
		drivers: make(map[string]db.Driver),
	}
	drv1 := &mockDriver{}
	m.drivers["conn1"] = drv1

	drv2, err := m.getOrCreateDriver(context.Background(), "conn1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if drv2 != drv1 {
		t.Fatal("expected returned driver to be the cached driver instance, but got a different one")
	}
}

func TestCopyErrorMsg(t *testing.T) {
	m := Model{
		results: results.New(theme.Theme{}),
	}
	res := &results.QueryResult{
		Error: "mock error details here",
	}
	m.results.SetResult(res)

	// Dispatch copyCellMsg
	m2, _ := m.Update(copyCellMsg{})
	typedM, ok := m2.(Model)
	if !ok {
		t.Fatalf("expected updated model to be Model, got %T", m2)
	}

	// The statusMsg should contain either "Error message copied" or "Clipboard unavailable"
	if !strings.Contains(typedM.statusMsg, "Error message") && !strings.Contains(typedM.statusMsg, "Clipboard unavailable") {
		t.Fatalf("expected status message for error copy, got: %q", typedM.statusMsg)
	}
}
