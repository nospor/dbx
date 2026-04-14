package querycontents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const contentsFile = "query-contents.json"
const diskVersion = 2

// Store holds per-connection/database editor buffer text on disk.
// Keys match history conn keys (e.g. "connID:database"); empty logical key is stored as "_".
type Store struct {
	path        string
	folderBased bool
	workDirKey  string

	global   map[string]string
	byFolder map[string]map[string]string
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

type diskFile struct {
	Version  int                          `json:"version,omitempty"`
	Global   map[string]string            `json:"global"`
	ByFolder map[string]map[string]string `json:"by_folder,omitempty"`
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
	return filepath.Join(cache, "dbx", contentsFile), nil
}

// New loads ~/.cache/dbx/query-contents.json. Corrupt files are backed up and replaced.
// folderBased selects per-startup-directory draft maps keyed by workDir.
func New(folderBased bool, workDir string) (*Store, error) {
	path, err := cacheDBXPath()
	if err != nil {
		return nil, err
	}
	s := &Store{
		path:        path,
		folderBased: folderBased,
		workDirKey:  normalizeWorkDir(workDir),
		global:      make(map[string]string),
		byFolder:    make(map[string]map[string]string),
	}
	if err := s.load(); err != nil {
		_ = os.Rename(path, path+".bak")
		s.global = make(map[string]string)
		s.byFolder = make(map[string]map[string]string)
		return s, nil
	}
	_ = s.ensureFile()
	return s, nil
}

// NewOrEmpty returns a Store that is always non-nil.
func NewOrEmpty(folderBased bool, workDir string) *Store {
	s, err := New(folderBased, workDir)
	if err != nil || s == nil {
		path := filepath.Join(".cache", "dbx", contentsFile)
		if p, err2 := cacheDBXPath(); err2 == nil {
			path = p
		}
		out := &Store{
			path:        path,
			folderBased: folderBased,
			workDirKey:  normalizeWorkDir(workDir),
			global:      make(map[string]string),
			byFolder:    make(map[string]map[string]string),
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
	return s.writeDisk()
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
	global, byFolder, err := parseQueryContentsData(data)
	if err != nil {
		return err
	}
	s.global = global
	if byFolder == nil {
		byFolder = make(map[string]map[string]string)
	}
	s.byFolder = byFolder
	return nil
}

func parseQueryContentsData(data []byte) (global map[string]string, byFolder map[string]map[string]string, err error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, nil, err
	}
	if _, ok := probe["global"]; ok || probe["by_folder"] != nil || probe["version"] != nil {
		var f diskFile
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, nil, err
		}
		if f.Global == nil {
			f.Global = make(map[string]string)
		}
		if f.ByFolder == nil {
			f.ByFolder = make(map[string]map[string]string)
		}
		return f.Global, f.ByFolder, nil
	}
	// Legacy: entire file is conn_key -> text
	legacy := make(map[string]string)
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, nil, err
	}
	return legacy, make(map[string]map[string]string), nil
}

func (s *Store) scopedTabs() map[string]string {
	if s == nil {
		return nil
	}
	if s.folderBased && s.workDirKey != "" {
		m := s.byFolder[s.workDirKey]
		if m == nil {
			return make(map[string]string)
		}
		return m
	}
	return s.global
}

func (s *Store) writeDisk() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if s.global == nil {
		s.global = make(map[string]string)
	}
	if s.byFolder == nil {
		s.byFolder = make(map[string]map[string]string)
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

// All returns a shallow copy of stored tab text for the active scope.
func (s *Store) All() map[string]string {
	tabs := s.scopedTabs()
	if len(tabs) == 0 {
		return nil
	}
	out := make(map[string]string, len(tabs))
	for k, v := range tabs {
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
	var target map[string]string
	if s.folderBased && s.workDirKey != "" {
		if s.byFolder[s.workDirKey] == nil {
			if s.byFolder == nil {
				s.byFolder = make(map[string]map[string]string)
			}
			s.byFolder[s.workDirKey] = make(map[string]string)
		}
		target = s.byFolder[s.workDirKey]
	} else {
		if s.global == nil {
			s.global = make(map[string]string)
		}
		target = s.global
	}
	target[connKey] = text
	return s.writeDisk()
}
