package cmd

import (
	"os/exec"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a background process",
	Run: func(cmd *cobra.Command, args []string) {
		// Example background process spawning
		// (e.g. running a sleep command, using the platform-independent SysProcAttr configuration)
		runCmd := exec.Command("sleep", "10")
		configureSysProcAttr(runCmd)
		_ = runCmd.Start()
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
