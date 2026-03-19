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

// New loads the history file. If it fails (e.g. corrupt), returns a fresh History with the path.
// Ensures the file exists (empty []) so it can be written later.
func New() (*History, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".config", "dbx", historyFile)
	h := &History{path: path, entries: []Entry{}}
	if err := h.load(); err != nil {
		// Corrupt or unreadable — backup and start fresh
		_ = os.Rename(path, path+".bak")
		return h, nil
	}
	_ = h.EnsureFile()
	return h, nil
}

// NewOrEmpty returns a History that never fails. Use when New fails (e.g. UserHomeDir error).
func NewOrEmpty() *History {
	h, _ := New()
	if h != nil {
		_ = h.EnsureFile()
		return h
	}
	path := ".config/dbx/history.json"
	if home, err := os.UserHomeDir(); err == nil {
		path = filepath.Join(home, ".config", "dbx", historyFile)
	}
	hist := &History{path: path, entries: []Entry{}}
	_ = hist.EnsureFile()
	return hist
}

// EnsureFile creates the history file with an empty array if it does not exist.
func (h *History) EnsureFile() error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return err
	}
	_, err := os.Stat(h.path)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(h.path, []byte("[]"), 0o600)
}

func (h *History) load() error {
	data, err := os.ReadFile(h.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
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
	// Atomic write: write to temp then rename
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, h.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Add appends a query to history and persists to disk.
func (h *History) Add(connKey, query string) error {
	if connKey == "" || query == "" {
		return nil
	}
	if h.entries == nil {
		h.entries = []Entry{}
	}
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
	if h.entries == nil {
		return nil
	}
	var result []Entry
	for i := len(h.entries) - 1; i >= 0; i-- {
		if h.entries[i].ConnKey == connKey {
			result = append(result, h.entries[i])
		}
	}
	return result
}
