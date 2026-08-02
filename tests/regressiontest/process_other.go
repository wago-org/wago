//go:build !linux && !darwin

package regressiontest

import "os/exec"

func PrepareCommand(cmd *exec.Cmd) {}
