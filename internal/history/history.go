package history

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const historyFile = "history.json"

// Entry is a single history record.
type Entry struct {
	ConnKey   string    `json:"conn_key"`   // "connID:database"
	Query     string    `json:"query"`
	CreatedAt time.Time `json:"created_at"`
}

// History manages query history persisted to ~/.config/dbx/history.json.
type History struct {
	entries []Entry
	path    string
}

// New loads (or creates) the history file.
func New() (*History, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".config", "dbx", historyFile)
	h := &History{path: path}
	if err := h.load(); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *History) load() error {
	data, err := os.ReadFile(h.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &h.entries)
}

func (h *History) save() error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(h.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.path, data, 0o600)
}

// Add appends a query to history and persists to disk.
func (h *History) Add(connKey, query string) error {
	// Avoid duplicate consecutive entries
	if len(h.entries) > 0 && h.entries[len(h.entries)-1].Query == query &&
		h.entries[len(h.entries)-1].ConnKey == connKey {
		return nil
	}
	h.entries = append(h.entries, Entry{
		ConnKey:   connKey,
		Query:     query,
		CreatedAt: time.Now(),
	})
	// Keep at most 1000 entries
	if len(h.entries) > 1000 {
		h.entries = h.entries[len(h.entries)-1000:]
	}
	return h.save()
}

// ForKey returns all history entries for a given connection key, newest first.
func (h *History) ForKey(connKey string) []Entry {
	var result []Entry
	for i := len(h.entries) - 1; i >= 0; i-- {
		if h.entries[i].ConnKey == connKey {
			result = append(result, h.entries[i])
		}
	}
	return result
}
