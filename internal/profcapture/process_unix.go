//go:build linux || darwin

package profcapture

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// A converter or collector may have children holding pipes or running the guest.
// Cancel the private process group so timeout cannot leave those children alive.
func boundProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second
}
