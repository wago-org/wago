//go:build darwin

package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type retainedReplaceHandle struct {
	state *darwinRetainedReplacement
}

type darwinRetainedReplacement struct {
	private     darwinPrivateDirectory
	temporary   string
	destination string
	stageStat   unix.Stat_t
	published   bool
	removed     bool
}

func createRetainedReplacementTemp(destination string, requireExistingParent bool) (*os.File, retainedReplaceHandle, error) {
	directory := filepath.Dir(destination)
	if !requireExistingParent {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, retainedReplaceHandle{}, err
		}
	}
	// Darwin ACL inheritance can grant access independently of mode 0600. Keep all
	// artifact bytes behind an ACL-free mode-0700 directory until
	// renameat publishes the complete file. All publication and cleanup operations
	// remain relative to pinned directory descriptors.
	private, err := createDarwinPrivateDirectory(directory, ".wago-atomic-")
	if err != nil {
		return nil, retainedReplaceHandle{}, fmt.Errorf("create private atomic staging directory: %w", err)
	}
	const temporary = "artifact"
	// ReplaceFile exposes only io.Writer semantics. O_WRONLY matches the former
	// os.WriteFile authorization and still permits chmod, sync, and metadata
	// restoration when a restrictive umask makes the fresh inode mode 000.
	fd, err := unix.Openat(int(private.file.Fd()), temporary,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, retainedReplaceHandle{}, errors.Join(
			fmt.Errorf("create private atomic staging file: %w", err), private.removeAndClose())
	}
	path := filepath.Join(directory, private.name, temporary)
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, retainedReplaceHandle{}, errors.Join(
			errors.New("wrap private atomic staging file"), private.removeAndClose())
	}
	state := &darwinRetainedReplacement{
		private: private, temporary: temporary, destination: filepath.Clean(destination),
	}
	if err := unix.Fstat(fd, &state.stageStat); err != nil {
		return nil, retainedReplaceHandle{}, errors.Join(err, file.Close(), private.removeAndClose())
	}
	return file, retainedReplaceHandle{state: state}, nil
}

func (handle retainedReplaceHandle) valid() bool { return handle.state != nil }

func (handle retainedReplaceHandle) replace(destination string) error {
	state := handle.state
	if state == nil {
		return errors.New("missing retained Darwin replacement")
	}
	if filepath.Clean(destination) != state.destination {
		return fmt.Errorf("retained atomic destination changed from %s to %s", state.destination, destination)
	}
	if filepath.Clean(filepath.Dir(destination)) != filepath.Clean(state.private.parentPath) {
		return errors.New("retained atomic destination parent changed")
	}
	// Reopen the requested parent immediately before publication and compare it
	// with the directory pinned at stage creation. Otherwise a swap-and-restore
	// sequence could make snapshot validation pass for A while renameat silently
	// publishes into detached B. A pathname swap after this comparison remains the
	// narrow documented portable compare-to-rename race, but renameat itself stays
	// bound to the validated inode.
	currentParentFD, err := unix.Open(filepath.Dir(destination),
		darwinOSearch|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	var currentParent unix.Stat_t
	statErr := unix.Fstat(currentParentFD, &currentParent)
	closeErr := unix.Close(currentParentFD)
	if statErr != nil || closeErr != nil {
		return errors.Join(statErr, closeErr)
	}
	if currentParent.Dev != state.private.parentStat.Dev || currentParent.Ino != state.private.parentStat.Ino {
		return errors.New("retained atomic destination parent changed")
	}
	var stage unix.Stat_t
	if err := unix.Fstatat(int(state.private.file.Fd()), state.temporary, &stage, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if stage.Dev != state.stageStat.Dev || stage.Ino != state.stageStat.Ino || stage.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("atomic temporary file changed before publication")
	}
	// The creator-only directory prevents a different identity that can write the
	// output parent from reaching this source. The identity comparison rejects any
	// earlier substitution; same-identity processes can still race renameat itself
	// and are outside that filesystem permission boundary.
	if err := unix.Renameat(int(state.private.file.Fd()), state.temporary,
		int(state.private.parent.Fd()), filepath.Base(destination)); err != nil {
		return err
	}
	state.published = true
	// Publication is already committed. Remove the now-empty private directory
	// promptly, but do not turn a cleanup-only race into a false report that the
	// destination was not updated. close retries this best-effort cleanup.
	if err := removeDarwinPrivateDirectory(state.private); err == nil {
		state.removed = true
	}
	return nil
}

func (handle retainedReplaceHandle) remove() error {
	state := handle.state
	if state == nil || state.removed {
		return nil
	}
	if !state.published {
		var stage unix.Stat_t
		stageErr := unix.Fstatat(int(state.private.file.Fd()), state.temporary, &stage, unix.AT_SYMLINK_NOFOLLOW)
		if stageErr != nil && !errors.Is(stageErr, unix.ENOENT) {
			return stageErr
		}
		if stageErr == nil {
			if stage.Dev != state.stageStat.Dev || stage.Ino != state.stageStat.Ino || stage.Mode&unix.S_IFMT != unix.S_IFREG {
				return errors.New("atomic temporary file changed before cleanup")
			}
			if err := unix.Unlinkat(int(state.private.file.Fd()), state.temporary, 0); err != nil && !errors.Is(err, unix.ENOENT) {
				return err
			}
		}
	}
	if err := removeDarwinPrivateDirectory(state.private); err != nil {
		return err
	}
	state.removed = true
	return nil
}

func (handle retainedReplaceHandle) close() error {
	state := handle.state
	if state == nil {
		return nil
	}
	var cleanupErr error
	if state.published && !state.removed {
		// The complete artifact is already visible. A concurrent directory-name
		// exchange can at worst leave an empty owner-only directory, not invalidate
		// publication or redirect deletion onto user data.
		if err := removeDarwinPrivateDirectory(state.private); err == nil {
			state.removed = true
		}
	} else if !state.published && !state.removed {
		cleanupErr = handle.remove()
	}
	return errors.Join(cleanupErr, state.private.close())
}
