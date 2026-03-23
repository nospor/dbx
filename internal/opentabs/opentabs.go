package opentabs

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const fileName = "open-tabs.json"

// snapshot is the on-disk JSON shape (v2). Legacy files are a bare JSON array of keys.
type snapshot struct {
	Keys   []string `json:"keys"`
	Active string   `json:"active,omitempty"`
}

// Store persists ordered connection:database keys for restored editor tabs and which tab was active.
type Store struct {
	path       string
	keys       []string
	activeKey  string
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
		s.activeKey = ""
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
	return s.saveSnapshot(nil, "")
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	keys, active, err := parseOpenTabsFile(data)
	if err != nil {
		return err
	}
	s.keys = keys
	s.activeKey = active
	return nil
}

func parseOpenTabsFile(data []byte) (keys []string, active string, err error) {
	trim := bytes.TrimSpace(data)
	if len(trim) == 0 {
		return nil, "", nil
	}
	if trim[0] == '[' {
		if err := json.Unmarshal(trim, &keys); err != nil {
			return nil, "", err
		}
		return keys, "", nil
	}
	var snap snapshot
	if err := json.Unmarshal(trim, &snap); err != nil {
		return nil, "", err
	}
	return snap.Keys, snap.Active, nil
}

func (s *Store) saveSnapshot(keys []string, activeKey string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot{Keys: keys, Active: activeKey}, "", "  ")
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

// ActiveKey returns the last saved active tab connection key (connID:db), or "" if none / legacy file.
func (s *Store) ActiveKey() string {
	if s == nil {
		return ""
	}
	return s.activeKey
}

// Save persists the ordered tab keys and which tab was active (conn key; empty if none).
func (s *Store) Save(keys []string, activeKey string) error {
	if s == nil {
		return nil
	}
	s.keys = append([]string(nil), keys...)
	s.activeKey = activeKey
	return s.saveSnapshot(s.keys, s.activeKey)
}
