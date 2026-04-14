package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/robertn/dbx/internal/app"
	"github.com/robertn/dbx/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	if err := config.EnsureDir(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create config dir: %v\n", err)
		os.Exit(1)
	}

	workDir, _ := os.Getwd()
	model := app.New(cfg, workDir)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running dbx: %v\n", err)
		os.Exit(1)
	}
}
