package app

import (
	"strings"

	"github.com/robertn/dbx/internal/config"
)

func splitConnKey(connKey string) (connID, dbName string) {
	if connKey == "" {
		return "", ""
	}
	parts := strings.SplitN(connKey, ":", 2)
	connID = parts[0]
	if len(parts) > 1 {
		dbName = parts[1]
	}
	return connID, dbName
}

func tabLabelForConfig(cfg *config.Config, connKey string) string {
	connID, db := splitConnKey(connKey)
	for _, c := range cfg.Connections {
		if c.ID == connID {
			label := c.Name
			if label == "" {
				label = connID
			}
			if db != "" {
				return label + " / " + db
			}
			return label
		}
	}
	if db != "" {
		return connID + " / " + db
	}
	return connKey
}
