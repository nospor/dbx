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

// Config is the root configuration structure persisted to ~/.config/dbx/config.json.
type Config struct {
	Connections []Connection `json:"connections"`
	Layout      Layout       `json:"layout"`
	Theme       string       `json:"theme"` // "dark", "light", "terminal"
	StatusMessageSeconds int `json:"status_message_seconds"` // default 5
}
