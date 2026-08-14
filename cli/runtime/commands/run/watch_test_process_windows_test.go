//go:build windows && !tinygo

package run

import "os/exec"

func detachWatchHelperProcess(*exec.Cmd) {}
