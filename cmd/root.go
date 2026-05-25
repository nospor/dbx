package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/robertn/dbx/internal/app"
	"github.com/robertn/dbx/internal/config"
)

var Version = "dev"

var rootCmd = &cobra.Command{
	Use:     "dbx",
	Short:   "A database client TUI",
	Version: Version,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if err := config.EnsureDir(); err != nil {
			return fmt.Errorf("failed to create config dir: %w", err)
		}

		workDir, _ := os.Getwd()
		model := app.New(cfg, workDir)

		p := tea.NewProgram(
			model,
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
		)

		if _, err := p.Run(); err != nil {
			return fmt.Errorf("error running dbx: %w", err)
		}
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
