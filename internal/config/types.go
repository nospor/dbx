package config

// Connection holds the settings for a single database connection.
type Connection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Driver   string `json:"driver"` // postgres, mysql, sqlite, mssql, mongodb, orientdb
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"` // comma-separated; empty = all databases
	SSLMode  string `json:"ssl_mode,omitempty"`
	FilePath string `json:"file_path,omitempty"` // for sqlite
}

// Layout holds pane size configuration (all values are percentages).
type Layout struct {
	ExplorerWidthPct int `json:"explorer_width_pct"` // default 25
	EditorHeightPct  int `json:"editor_height_pct"`  // default 50
	AIPaneWidthPct   int `json:"ai_pane_width_pct"`  // default 25
	// ExplorerHidden / AIPaneHidden use pointers so JSON omission can mean "use default"
	// (explorer shown, AI pane hidden).
	ExplorerHidden *bool `json:"explorer_hidden,omitempty"`
	AIPaneHidden   *bool `json:"ai_pane_hidden,omitempty"`
}

// Config is the root runtime configuration.
// UI preferences are persisted to ~/.config/dbx/config.json.
// Connections are persisted separately to ~/.cache/dbx/connections.json.
type Config struct {
	Connections          []Connection `json:"-"`
	Layout               Layout       `json:"layout"`
	Theme                string       `json:"theme"`                  // terminal, dark, light, catppuccin-mocha, catppuccin-latte, nord, gruvbox-dark
	StatusMessageSeconds int          `json:"status_message_seconds"` // default 5
	// FolderBased, when true, scopes restored query tabs and per-tab editor drafts to the
	// process working directory at startup (still stored under the XDG cache path, not in the project folder).
	FolderBased bool      `json:"folder_based"`
	AI          *AIConfig `json:"ai,omitempty"`
}

type AIAppConfig struct {
	ModelsCommand        string `json:"models_command"`
	ModelsResponseFormat string `json:"models_response_format"`
	CreateSessionCommand string `json:"create_session_command"`
	SessionModeFlag      string `json:"session_mode_flag"`   // e.g. "--mode ask"
	ResumeSessionFlag    string `json:"resume_session_flag"` // e.g. "--resume"
	ModelFlag            string `json:"model_flag"`          // e.g. "--model"
}

type AIConfig struct {
	SelectedApp         string                 `json:"selected_app"`
	MaxHistorySizeKB    int                    `json:"max_history_size_kb"`
	MaxResultsContextKB int                    `json:"max_results_context_kb,omitempty"` // cap for /results query+grid sent to AI; default 256
	Apps                map[string]AIAppConfig `json:"apps"`
	// DisableAgentWorkdir, when true, runs the AI CLI with the process default cwd (same as dbx).
	// When false, dbx sets cmd.Dir to AgentWorkdir (or the default cache path if AgentWorkdir is empty).
	DisableAgentWorkdir bool   `json:"disable_agent_workdir"`
	AgentWorkdir        string `json:"agent_workdir"` // empty = ~/.cache/dbx/aiagentfolder (or XDG cache equivalent)
}
