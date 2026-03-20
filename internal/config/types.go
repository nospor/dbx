package config

// Connection holds the settings for a single database connection.
type Connection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Driver   string `json:"driver"` // postgres, mysql, sqlite, mssql
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
}

// Config is the root runtime configuration.
// UI preferences are persisted to ~/.config/dbx/config.json.
// Connections are persisted separately to ~/.cache/dbx/connections.json.
type Config struct {
	Connections []Connection `json:"-"`
	Layout      Layout       `json:"layout"`
	Theme       string       `json:"theme"` // terminal, dark, light, catppuccin-mocha, catppuccin-latte, nord, gruvbox-dark
	StatusMessageSeconds int `json:"status_message_seconds"` // default 5
}
