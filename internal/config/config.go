package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	if needPersistAI {
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
	return &Config{
		Connections: []Connection{},
		Layout: Layout{
			ExplorerWidthPct: 25,
			EditorHeightPct:  50,
			AIPaneWidthPct:   25,
		},
		Theme:                "terminal",
		StatusMessageSeconds: 5,
		AI: &AIConfig{
			SelectedApp:           "cursor-agent",
			MaxHistorySizeKB:      1024,
			MaxResultsContextKB:   256,
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
		},
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
