//go:build windows

package cmd

import (
	"os/exec"
)

func configureSysProcAttr(cmd *exec.Cmd) {
	// On Windows, Setsid is unsupported. cmd.Start() is sufficient
	// to start the command asynchronously in the background.
}
