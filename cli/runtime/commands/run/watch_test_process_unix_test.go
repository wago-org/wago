//go:build linux && !tinygo && !wago_lean

package run

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/wago-org/wago/cli/internal/watchsupervisor"
)

func detachWatchHelperProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func configureWatchTestSupervisor(options *watchOptions) {
	options.environment = watchsupervisor.Environment(options.environment, os.Args[0])
}
