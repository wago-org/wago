//go:build !linux && !darwin

package wasmtimetest

import "os/exec"

func PrepareCommand(cmd *exec.Cmd) {}
