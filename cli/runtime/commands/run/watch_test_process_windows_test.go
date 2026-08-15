//go:build windows && !tinygo && !wago_lean

package run

import "os/exec"

func detachWatchHelperProcess(*exec.Cmd) {}

func configureWatchTestSupervisor(*watchOptions) {}
