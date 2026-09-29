//go:build !linux && !darwin

package profcapture

import (
	"os/exec"
	"time"
)

func boundProcess(cmd *exec.Cmd) { cmd.WaitDelay = 2 * time.Second }
