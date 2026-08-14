//go:build (linux || darwin) && !tinygo

package run

import (
	"os/exec"
	"syscall"
)

func detachWatchHelperProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
