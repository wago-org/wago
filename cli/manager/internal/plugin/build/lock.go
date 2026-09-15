package build

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	buildLockPoll    = 50 * time.Millisecond
	buildLockTimeout = 30 * time.Minute
)

// withBuildLock serializes access to a generated plugin build module across
// Wago processes. Go deliberately refuses to overwrite go.mod when another
// process changes it during `go mod edit`; keeping every module operation under
// one portable mkdir lock also protects main.go and the cached binary/hash pair.
func withBuildLock(dir string, fn func() error) error {
	lockDir := dir + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockDir), 0o755); err != nil {
		return err
	}
	if err := acquireBuildLock(lockDir, os.Mkdir); err != nil {
		return err
	}
	defer os.Remove(lockDir)
	return fn()
}

func acquireBuildLock(lockDir string, mkdir func(string, os.FileMode) error) error {
	deadline := time.Now().Add(buildLockTimeout)
	retriedPermission := false
	for {
		err := mkdir(lockDir, 0o755)
		if err == nil {
			return nil
		}
		if buildLockContended(lockDir, err) {
			retriedPermission = false
		} else {
			// Windows can deny mkdir during removal, then report the lock
			// absent to Stat. Retry once; persistent denial stays an error.
			if !os.IsPermission(err) || retriedPermission {
				return fmt.Errorf("lock plugin build: %w", err)
			}
			retriedPermission = true
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("plugin build lock timed out; if no Wago process is running, remove %s", lockDir)
		}
		time.Sleep(buildLockPoll)
	}
}

func buildLockContended(lockDir string, mkdirErr error) bool {
	if os.IsExist(mkdirErr) {
		return true
	}
	// Windows may report ERROR_ACCESS_DENIED while another process owns the
	// existing lock directory. A successful stat still proves contention;
	// other permission failures remain hard errors.
	_, err := os.Stat(lockDir)
	return err == nil
}
