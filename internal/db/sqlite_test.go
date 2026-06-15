package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/robertn/dbx/internal/config"
)

func TestResolveSQLitePath(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		want    string
		wantErr bool
	}{
		{
			name: ":memory:",
			dsn:  ":memory:",
			want: ":memory:",
		},
		{
			name: "memory param",
			dsn:  "file:test.db?mode=memory",
			want: "file:test.db?mode=memory",
		},
		{
			name: "simple relative path",
			dsn:  "test_db.sqlite",
			want: "test_db.sqlite",
		},
		{
			name: "relative path with query params",
			dsn:  "test_db.sqlite?cache=shared",
			want: "test_db.sqlite?cache=shared",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSQLitePath(tt.dsn)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveSQLitePath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("resolveSQLitePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveSQLitePathWithTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("skipping test; user home directory not found")
	}

	tests := []struct {
		name    string
		dsn     string
		want    string
		wantErr bool
	}{
		{
			name: "tilde",
			dsn:  "~",
			want: home,
		},
		{
			name: "tilde slash",
			dsn:  "~/mydb.sqlite",
			want: filepath.Join(home, "mydb.sqlite"),
		},
		{
			name: "tilde slash query params",
			dsn:  "~/mydb.sqlite?cache=shared",
			want: filepath.Join(home, "mydb.sqlite") + "?cache=shared",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSQLitePath(tt.dsn)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveSQLitePath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("resolveSQLitePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSQLiteConnect(t *testing.T) {
	driver := &sqliteDriver{}
	ctx := context.Background()

	t.Run("memory database succeeds", func(t *testing.T) {
		conn := config.Connection{
			Driver:   "sqlite",
			FilePath: ":memory:",
		}
		err := driver.Connect(ctx, conn)
		if err != nil {
			t.Fatalf("expected memory database connection to succeed, got %v", err)
		}
		driver.Close()
	})

	t.Run("non-existent database file fails", func(t *testing.T) {
		conn := config.Connection{
			Driver:   "sqlite",
			FilePath: "this_file_does_not_exist_at_all.sqlite",
		}
		err := driver.Connect(ctx, conn)
		if err == nil {
			t.Fatal("expected connection to fail for non-existent file, but it succeeded")
		}
		if err.Error() != "sqlite: database file \"this_file_does_not_exist_at_all.sqlite\" does not exist" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("existing database file succeeds", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "dbx-sqlite-test")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		dbPath := filepath.Join(tempDir, "test.db")
		f, err := os.Create(dbPath)
		if err != nil {
			t.Fatalf("failed to create temp db file: %v", err)
		}
		f.Close()

		conn := config.Connection{
			Driver:   "sqlite",
			FilePath: dbPath,
		}
		err = driver.Connect(ctx, conn)
		if err != nil {
			t.Fatalf("expected connection to succeed for existing file, got %v", err)
		}
		driver.Close()
	})
}
