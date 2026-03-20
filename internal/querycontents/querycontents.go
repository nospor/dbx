package querycontents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const contentsFile = "query-contents.json"

// Store holds per-connection/database editor buffer text on disk.
// Keys match history conn keys (e.g. "connID:database"); empty logical key is stored as "_".
type Store struct {
	path string
	tabs map[string]string // conn_key -> full editor text
}

// New loads ~/.cache/dbx/query-contents.json. Corrupt files are backed up and replaced.
func New() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = filepath.Join(home, ".cache")
	}
	path := filepath.Join(cache, "dbx", contentsFile)
	s := &Store{path: path, tabs: make(map[string]string)}
	if err := s.load(); err != nil {
		_ = os.Rename(path, path+".bak")
		s.tabs = make(map[string]string)
		return s, nil
	}
	_ = s.ensureFile()
	return s, nil
}

// NewOrEmpty returns a Store that is always non-nil.
func NewOrEmpty() *Store {
	s, err := New()
	if err != nil || s == nil {
		path := ".cache/dbx/" + contentsFile
		if home, err2 := os.UserHomeDir(); err2 == nil {
			cache, err3 := os.UserCacheDir()
			if err3 != nil {
				cache = filepath.Join(home, ".cache")
			}
			path = filepath.Join(cache, "dbx", contentsFile)
		}
		out := &Store{path: path, tabs: make(map[string]string)}
		_ = out.ensureFile()
		return out
	}
	return s
}

func (s *Store) ensureFile() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	_, err := os.Stat(s.path)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.MarshalIndent(map[string]string{}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &s.tabs); err != nil {
		return err
	}
	if s.tabs == nil {
		s.tabs = make(map[string]string)
	}
	return nil
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.tabs, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// All returns a shallow copy of stored tab text.
func (s *Store) All() map[string]string {
	if s == nil || s.tabs == nil {
		return nil
	}
	out := make(map[string]string, len(s.tabs))
	for k, v := range s.tabs {
		out[k] = v
	}
	return out
}

// Put stores text for connKey and persists (atomic write).
func (s *Store) Put(connKey, text string) error {
	if s == nil {
		return nil
	}
	if connKey == "" {
		connKey = "_"
	}
	if s.tabs == nil {
		s.tabs = make(map[string]string)
	}
	s.tabs[connKey] = text
	return s.save()
}
