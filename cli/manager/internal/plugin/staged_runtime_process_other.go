//go:build !linux && !darwin && !windows

package plugin

import "os/exec"

// Wago releases target Linux, Darwin, and Windows. Keep unsupported ports
// buildable while CommandContext still bounds the direct validation process.
func runBoundStagedRuntime(command *exec.Cmd) error { return command.Run() }
