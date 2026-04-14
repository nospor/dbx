package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const configDir = ".config/dbx"
const configFile = "config.json"
const cacheDir = ".cache/dbx"
const connectionsFile = "connections.json"

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configDir, configFile), nil
}

func connectionsPath() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		home, err2 := os.UserHomeDir()
		if err2 != nil {
			return "", err
		}
		return filepath.Join(home, cacheDir, connectionsFile), nil
	}
	return filepath.Join(cache, "dbx", connectionsFile), nil
}

// EnsureDir creates storage directories used by dbx.
func EnsureDir() error {
	cfgPath, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return err
	}
	connPath, err := connectionsPath()
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Dir(connPath), 0o755)
}

// DefaultAIAgentWorkDir returns the default isolated cwd for AI CLI subprocesses
// (same base location as other dbx cache files: XDG_CACHE_HOME/dbx/aiagentfolder).
func DefaultAIAgentWorkDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(home, cacheDir, "aiagentfolder"), nil
	}
	return filepath.Join(cache, "dbx", "aiagentfolder"), nil
}

func expandAgentWorkdir(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("empty agent workdir")
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[2:])
	}
	p = os.ExpandEnv(p)
	return filepath.Clean(p), nil
}

// ResolveAIAgentWorkDir returns the directory to pass as cmd.Dir for AI subprocesses.
// If ok is false, the caller should leave cmd.Dir unset (inherit dbx's cwd).
func (c *Config) ResolveAIAgentWorkDir() (dir string, ok bool, err error) {
	if c == nil || c.AI == nil || c.AI.DisableAgentWorkdir {
		return "", false, nil
	}
	if strings.TrimSpace(c.AI.AgentWorkdir) == "" {
		d, err := DefaultAIAgentWorkDir()
		return d, true, err
	}
	d, err := expandAgentWorkdir(c.AI.AgentWorkdir)
	return d, true, err
}

func aiSectionMissingAgentWorkdirKeys(aiJSON []byte) bool {
	if len(aiJSON) == 0 {
		return false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(aiJSON, &m); err != nil {
		return false
	}
	_, hasDis := m["disable_agent_workdir"]
	_, hasPath := m["agent_workdir"]
	return !hasDis || !hasPath
}

// Load reads the config from disk. Returns defaults if the file does not exist.
func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := defaults()
		applyDefaults(cfg)
		if err := Save(cfg); err != nil {
			return nil, fmt.Errorf("write initial config: %w", err)
		}
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	type legacyConfig struct {
		Connections          []Connection `json:"connections"`
		Layout               Layout       `json:"layout"`
		Theme                string       `json:"theme"`
		StatusMessageSeconds int          `json:"status_message_seconds"`
		AI                   *AIConfig    `json:"ai,omitempty"`
	}
	var legacy legacyConfig
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}
	var topProbe struct {
		AI json.RawMessage `json:"ai,omitempty"`
	}
	_ = json.Unmarshal(data, &topProbe)
	needPersistAIAgentWD := aiSectionMissingAgentWorkdirKeys(topProbe.AI)

	needPersistAI := legacy.AI == nil || legacy.AI.Apps == nil
	cfg := Config{
		Connections:          legacy.Connections,
		Layout:               legacy.Layout,
		Theme:                legacy.Theme,
		StatusMessageSeconds: legacy.StatusMessageSeconds,
		AI:                   legacy.AI,
	}
	if conns, err := loadConnections(); err == nil {
		cfg.Connections = conns
	} else if len(cfg.Connections) > 0 {
		// Backward-compatible migration from old config.json field.
		_ = saveConnections(cfg.Connections)
	}
	applyDefaults(&cfg)
	if needPersistAIAgentWD && cfg.AI != nil {
		if !cfg.AI.DisableAgentWorkdir && strings.TrimSpace(cfg.AI.AgentWorkdir) == "" {
			if p, err := DefaultAIAgentWorkDir(); err == nil {
				cfg.AI.AgentWorkdir = p
			}
		}
	}
	if needPersistAI || needPersistAIAgentWD {
		if err := Save(&cfg); err != nil {
			return nil, fmt.Errorf("persist default AI settings: %w", err)
		}
	}
	return &cfg, nil
}

// Save writes the config to disk, creating directories as needed.
func Save(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	type diskConfig struct {
		Layout               Layout    `json:"layout"`
		Theme                string    `json:"theme"`
		StatusMessageSeconds int       `json:"status_message_seconds"`
		AI                   *AIConfig `json:"ai,omitempty"`
	}
	out := diskConfig{
		Layout:               cfg.Layout,
		Theme:                cfg.Theme,
		StatusMessageSeconds: cfg.StatusMessageSeconds,
		AI:                   cfg.AI,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return saveConnections(cfg.Connections)
}

func defaults() *Config {
	aiCfg := &AIConfig{
		SelectedApp:         "cursor-agent",
		MaxHistorySizeKB:    1024,
		MaxResultsContextKB: 256,
		DisableAgentWorkdir: false,
		Apps: map[string]AIAppConfig{
			"cursor-agent": {
				ModelsCommand:        "cursor-agent models",
				ModelsResponseFormat: "Available models\n\n{models}\n\nTip: use --model <id> (or /model <id> in interactive mode) to switch.",
				CreateSessionCommand: "cursor-agent create-chat",
				SessionModeFlag:      "--mode ask",
				ResumeSessionFlag:    "--resume",
				ModelFlag:            "--model",
			},
		},
	}
	if p, err := DefaultAIAgentWorkDir(); err == nil {
		aiCfg.AgentWorkdir = p
	}
	return &Config{
		Connections: []Connection{},
		Layout: Layout{
			ExplorerWidthPct: 25,
			EditorHeightPct:  50,
			AIPaneWidthPct:   25,
		},
		Theme:                "terminal",
		StatusMessageSeconds: 5,
		AI:                   aiCfg,
	}
}

func loadConnections() ([]Connection, error) {
	path, err := connectionsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Connection{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return []Connection{}, nil
	}
	var conns []Connection
	if err := json.Unmarshal(data, &conns); err != nil {
		return nil, err
	}
	if conns == nil {
		conns = []Connection{}
	}
	return conns, nil
}

func saveConnections(conns []Connection) error {
	path, err := connectionsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if conns == nil {
		conns = []Connection{}
	}
	data, err := json.MarshalIndent(conns, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func applyDefaults(cfg *Config) {
	if cfg.Layout.ExplorerWidthPct == 0 {
		cfg.Layout.ExplorerWidthPct = 25
	}
	if cfg.Layout.EditorHeightPct == 0 {
		cfg.Layout.EditorHeightPct = 50
	}
	if cfg.Theme == "" {
		cfg.Theme = "terminal"
	}
	if cfg.StatusMessageSeconds <= 0 {
		cfg.StatusMessageSeconds = 5
	}
	if cfg.Layout.AIPaneWidthPct == 0 {
		cfg.Layout.AIPaneWidthPct = 25
	}
	if cfg.Layout.ExplorerHidden == nil {
		v := false
		cfg.Layout.ExplorerHidden = &v
	}
	if cfg.Layout.AIPaneHidden == nil {
		v := true
		cfg.Layout.AIPaneHidden = &v
	}
	if cfg.AI == nil {
		cfg.AI = defaults().AI
	} else if cfg.AI.Apps == nil {
		cfg.AI.Apps = defaults().AI.Apps
	}
	if cfg.AI.MaxResultsContextKB <= 0 {
		cfg.AI.MaxResultsContextKB = 256
	}
}
