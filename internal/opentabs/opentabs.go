package opentabs

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const fileName = "open-tabs.json"
const diskVersion = 2

// snapshot is one persisted tab set (ordered keys + active connID:database key).
type snapshot struct {
	Keys   []string          `json:"keys"`
	Active string            `json:"active,omitempty"`
	Titles map[string]string `json:"titles,omitempty"`
}

// diskFile is the on-disk JSON shape (v2). Legacy files are a bare array of keys or a root snapshot.
type diskFile struct {
	Version  int                 `json:"version,omitempty"`
	Global   snapshot            `json:"global"`
	ByFolder map[string]snapshot `json:"by_folder,omitempty"`
}

// Store persists ordered connection:database keys for restored editor tabs and which tab was active.
type Store struct {
	path        string
	folderBased bool
	workDirKey  string // absolute clean path; empty disables folder scoping even when folderBased

	global   snapshot
	byFolder map[string]snapshot
}

func normalizeWorkDir(wd string) string {
	wd = strings.TrimSpace(wd)
	if wd == "" {
		return ""
	}
	abs, err := filepath.Abs(wd)
	if err != nil {
		return filepath.Clean(wd)
	}
	return filepath.Clean(abs)
}

func cacheDBXPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = filepath.Join(home, ".cache")
	}
	return filepath.Join(cache, "dbx", fileName), nil
}

// New loads ~/.cache/dbx/open-tabs.json. folderBased selects per-startup-directory tab sets keyed by workDir.
func New(folderBased bool, workDir string) (*Store, error) {
	path, err := cacheDBXPath()
	if err != nil {
		return nil, err
	}
	s := &Store{
		path:        path,
		folderBased: folderBased,
		workDirKey:  normalizeWorkDir(workDir),
		byFolder:    make(map[string]snapshot),
	}
	if err := s.load(); err != nil {
		_ = os.Rename(path, path+".bak")
		s.global = snapshot{}
		s.byFolder = make(map[string]snapshot)
	}
	_ = s.ensureFile()
	return s, nil
}

// NewOrEmpty returns a non-nil Store.
func NewOrEmpty(folderBased bool, workDir string) *Store {
	s, err := New(folderBased, workDir)
	if err != nil || s == nil {
		path := filepath.Join(".cache", "dbx", fileName)
		if p, err2 := cacheDBXPath(); err2 == nil {
			path = p
		}
		out := &Store{
			path:        path,
			folderBased: folderBased,
			workDirKey:  normalizeWorkDir(workDir),
			byFolder:    make(map[string]snapshot),
		}
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
	return s.saveDisk()
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	global, byFolder, err := parseOpenTabsData(data)
	if err != nil {
		return err
	}
	s.global = global
	if byFolder == nil {
		byFolder = make(map[string]snapshot)
	}
	s.byFolder = byFolder
	return nil
}

func parseOpenTabsData(data []byte) (global snapshot, byFolder map[string]snapshot, err error) {
	trim := bytes.TrimSpace(data)
	if len(trim) == 0 {
		return snapshot{}, make(map[string]snapshot), nil
	}
	if trim[0] == '[' {
		var keys []string
		if err := json.Unmarshal(trim, &keys); err != nil {
			return snapshot{}, nil, err
		}
		return snapshot{Keys: keys}, make(map[string]snapshot), nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(trim, &probe); err != nil {
		return snapshot{}, nil, err
	}
	if _, ok := probe["global"]; ok || probe["by_folder"] != nil || probe["version"] != nil {
		var f diskFile
		if err := json.Unmarshal(trim, &f); err != nil {
			return snapshot{}, nil, err
		}
		if f.ByFolder == nil {
			f.ByFolder = make(map[string]snapshot)
		}
		return f.Global, f.ByFolder, nil
	}
	var snap snapshot
	if err := json.Unmarshal(trim, &snap); err != nil {
		return snapshot{}, nil, err
	}
	return snap, make(map[string]snapshot), nil
}

func (s *Store) currentSnap() snapshot {
	if s == nil {
		return snapshot{}
	}
	if s.folderBased && s.workDirKey != "" {
		v, ok := s.byFolder[s.workDirKey]
		if !ok {
			return snapshot{}
		}
		return v
	}
	return s.global
}

func (s *Store) saveDisk() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if s.byFolder == nil {
		s.byFolder = make(map[string]snapshot)
	}
	data, err := json.MarshalIndent(diskFile{
		Version:  diskVersion,
		Global:   s.global,
		ByFolder: s.byFolder,
	}, "", "  ")
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

// Keys returns the last saved order (copy) for the active scope.
func (s *Store) Keys() []string {
	cur := s.currentSnap()
	if len(cur.Keys) == 0 {
		return nil
	}
	out := make([]string, len(cur.Keys))
	copy(out, cur.Keys)
	return out
}

// Titles returns the last saved custom titles (copy) for the active scope.
func (s *Store) Titles() map[string]string {
	cur := s.currentSnap()
	if len(cur.Titles) == 0 {
		return nil
	}
	out := make(map[string]string, len(cur.Titles))
	for k, v := range cur.Titles {
		out[k] = v
	}
	return out
}

// ActiveKey returns the last saved active tab connection key (connID:db), or "" if none / legacy file.
func (s *Store) ActiveKey() string {
	return s.currentSnap().Active
}

// Save persists the ordered tab keys and which tab was active for the active scope.
func (s *Store) Save(keys []string, activeKey string, titles map[string]string) error {
	if s == nil {
		return nil
	}
	snap := snapshot{
		Keys:   append([]string(nil), keys...),
		Active: activeKey,
		Titles: titles,
	}
	if s.folderBased && s.workDirKey != "" {
		if s.byFolder == nil {
			s.byFolder = make(map[string]snapshot)
		}
		s.byFolder[s.workDirKey] = snap
	} else {
		s.global = snap
	}
	return s.saveDisk()
}
