//go:build !windows

package managedrelease

import (
	"fmt"
	"os"
	"time"
)

var localCleanupIdentity = uint64(time.Now().UnixNano())

func processIdentity(pid int) (uint64, bool, error) {
	// Deferred uninstall workers run only on Windows. Local scheduling tests still
	// get a process-lifetime identity; never guess foreign-owner liveness here.
	if pid == os.Getpid() {
		return localCleanupIdentity, true, nil
	}
	return 0, false, fmt.Errorf("deferred cleanup owner %d cannot be verified on this platform", pid)
}
