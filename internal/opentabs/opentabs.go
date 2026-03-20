package opentabs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const fileName = "open-tabs.json"

// Store persists ordered connection:database keys for restored editor tabs.
type Store struct {
	path string
	keys []string
}

// New loads ~/.cache/dbx/open-tabs.json.
func New() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = filepath.Join(home, ".cache")
	}
	path := filepath.Join(cache, "dbx", fileName)
	s := &Store{path: path}
	if err := s.load(); err != nil {
		_ = os.Rename(path, path+".bak")
		s.keys = nil
	}
	_ = s.ensureFile()
	return s, nil
}

// NewOrEmpty returns a non-nil Store.
func NewOrEmpty() *Store {
	s, err := New()
	if err != nil || s == nil {
		path := filepath.Join(".cache", "dbx", fileName)
		if home, err2 := os.UserHomeDir(); err2 == nil {
			if cache, err3 := os.UserCacheDir(); err3 == nil {
				path = filepath.Join(cache, "dbx", fileName)
			} else {
				path = filepath.Join(home, ".cache", "dbx", fileName)
			}
		}
		out := &Store{path: path}
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
	return s.saveBytes([]string{})
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
	var keys []string
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	s.keys = keys
	return nil
}

func (s *Store) saveBytes(keys []string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(keys, "", "  ")
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

// Keys returns the last saved order (copy).
func (s *Store) Keys() []string {
	if s == nil || len(s.keys) == 0 {
		return nil
	}
	out := make([]string, len(s.keys))
	copy(out, s.keys)
	return out
}

// Save persists the ordered tab keys.
func (s *Store) Save(keys []string) error {
	if s == nil {
		return nil
	}
	s.keys = append([]string(nil), keys...)
	return s.saveBytes(s.keys)
}
