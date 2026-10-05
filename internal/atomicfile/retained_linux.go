//go:build linux && !(tinygo && wago_lean && wago_minimal)

package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

type retainedReplaceHandle struct {
	state *linuxRetainedReplacement
}

type linuxRetainedReplacement struct {
	private     linuxPrivateDirectory
	parentPath  string
	destination string
	parent      linuxFileIdentity
	stage       linuxFileIdentity
}

func createRetainedReplacementTemp(destination string, requireExistingParent bool) (*os.File, retainedReplaceHandle, error) {
	directory := filepath.Dir(destination)
	if !requireExistingParent {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, retainedReplaceHandle{}, err
		}
	}
	parentFD, err := syscall.Openat(unix.AT_FDCWD, directory, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, retainedReplaceHandle{}, err
	}
	state := &linuxRetainedReplacement{parentPath: directory, destination: filepath.Clean(destination)}
	var parentStat syscall.Stat_t
	if err := syscall.Fstat(parentFD, &parentStat); err != nil {
		return nil, retainedReplaceHandle{}, joinErrors(err, syscall.Close(parentFD))
	}
	state.parent = linuxFileIdentity{dev: uint64(parentStat.Dev), ino: uint64(parentStat.Ino)}
	private, err := createLinuxPrivateDirectory(parentFD, ".wago-atomic-")
	if err != nil {
		return nil, retainedReplaceHandle{}, joinErrors(err, syscall.Close(parentFD))
	}
	state.private = private
	_, destinationErr := os.Lstat(destination)
	missing := os.IsNotExist(destinationErr)
	if destinationErr != nil && !missing {
		return nil, retainedReplaceHandle{}, joinErrors(destinationErr, closeUnusedLinuxPrivateDirectory(state))
	}
	fd := -1
	if missing {
		fd, err = createMissingLinuxRetainedStage(state)
	} else {
		fd, err = syscall.Openat(private.pinFD, "artifact",
			unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	}
	if err != nil {
		return nil, retainedReplaceHandle{}, joinErrors(err, closeUnusedLinuxPrivateDirectory(state))
	}
	var stageStat syscall.Stat_t
	if err := syscall.Fstat(fd, &stageStat); err != nil {
		return nil, retainedReplaceHandle{}, joinErrors(err, syscall.Close(fd), closeUnusedLinuxPrivateDirectory(state))
	}
	state.stage = linuxFileIdentity{dev: uint64(stageStat.Dev), ino: uint64(stageStat.Ino)}
	// fd came from a successful Openat above and is therefore valid for NewFile.
	file := os.NewFile(uintptr(fd), filepath.Join(directory, private.name, "artifact"))
	return file, retainedReplaceHandle{state: state}, nil
}

func createMissingLinuxRetainedStage(state *linuxRetainedReplacement) (int, error) {
	// Reuse the private directory's random suffix. A peer that races this
	// derived name can only force a safe O_EXCL failure before bytes are written.
	name := ".wago-inherit-" + state.private.name[len(".wago-atomic-"):]
	// Mode 000 lets the kernel attach exact direct-child ownership, ACL, and
	// security-label metadata while preventing any other unprivileged identity
	// from opening the inode before it is moved behind the private directory.
	fd, err := syscall.Openat(state.private.parentFD, name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	var opened syscall.Stat_t
	var linked unix.Stat_t
	openErr := syscall.Fstat(fd, &opened)
	sourceErr := unix.Fstatat(state.private.parentFD, name, &linked, unix.AT_SYMLINK_NOFOLLOW)
	if openErr != nil || sourceErr != nil || opened.Dev != linked.Dev || opened.Ino != linked.Ino ||
		linked.Mode&unix.S_IFMT != unix.S_IFREG || linked.Mode&0o777 != 0 ||
		opened.Nlink != 1 || linked.Nlink != 1 {
		return -1, joinErrors(formatErrorS("stage changed before isolation; recover at %s",
			filepath.Join(state.parentPath, name)), openErr, sourceErr, syscall.Close(fd))
	}
	// Record the inode before moving it so every later error path refuses to
	// delete a substituted private entry as though it were our empty stage.
	state.stage = linuxFileIdentity{dev: uint64(opened.Dev), ino: uint64(opened.Ino)}
	// The destination slot is new and protected by the private directory, so a
	// plain descriptor-relative rename cannot replace another identity's file.
	if err := syscall.Renameat(state.private.parentFD, name, state.private.pinFD, "artifact"); err != nil {
		return -1, joinErrors(formatErrorSE("isolate empty inherited stage at %s: %w",
			filepath.Join(state.parentPath, name), err), syscall.Close(fd))
	}
	movedErr := unix.Fstatat(state.private.pinFD, "artifact", &linked, unix.AT_SYMLINK_NOFOLLOW)
	if movedErr == nil && opened.Dev == linked.Dev && opened.Ino == linked.Ino &&
		linked.Mode&unix.S_IFMT == unix.S_IFREG && linked.Nlink == 1 {
		return fd, nil
	}
	recovery := filepath.Join(state.parentPath, state.private.name, "artifact")
	return -1, joinErrors(formatErrorS("stage changed after isolation; recover at %s", recovery),
		movedErr, syscall.Close(fd))
}

func closeUnusedLinuxPrivateDirectory(state *linuxRetainedReplacement) error {
	return (retainedReplaceHandle{state: state}).close()
}

func removeLinuxPrivateArtifact(state *linuxRetainedReplacement) error {
	if state.stage.dev == 0 && state.stage.ino == 0 {
		return nil
	}
	var stage unix.Stat_t
	err := unix.Fstatat(state.private.pinFD, "artifact", &stage, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	if !state.stage.matches(&stage) || stage.Mode&unix.S_IFMT != unix.S_IFREG {
		return newError("staged file changed before cleanup")
	}
	return syscall.Unlinkat(state.private.pinFD, "artifact")
}

func (handle retainedReplaceHandle) valid() bool { return handle.state != nil }

func (handle retainedReplaceHandle) replace(destination string) error {
	state := handle.state
	if state == nil {
		return newError("missing Linux replacement")
	}
	if filepath.Clean(destination) != state.destination ||
		filepath.Clean(filepath.Dir(destination)) != filepath.Clean(state.parentPath) {
		return newError("retained destination changed")
	}
	var current unix.Stat_t
	if err := unix.Fstatat(unix.AT_FDCWD, filepath.Dir(destination), &current, 0); err != nil {
		return err
	}
	if !state.parent.matches(&current) {
		return newError("retained destination parent changed")
	}
	if err := unix.Fstatat(state.private.pinFD, "artifact", &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if !state.stage.matches(&current) || current.Mode&unix.S_IFMT != unix.S_IFREG {
		return newError("staged file changed before publication")
	}
	// Cross-identity directory writers cannot traverse the creator-only staging
	// directory. The final identity comparison rejects any earlier substitution;
	// same-identity processes can still race renameat and are outside that boundary.
	if err := syscall.Renameat(state.private.pinFD, "artifact",
		state.private.parentFD, filepath.Base(destination)); err != nil {
		return err
	}
	// The artifact left the private directory. Clear its expected identity and
	// destination to mark committed publication. Close may retry directory cleanup,
	// but must not turn a successful atomic rename into a reported build failure.
	state.stage = linuxFileIdentity{}
	state.destination = ""
	if err := removeLinuxPrivateDirectory(state.private); err == nil {
		state.private.name = ""
	}
	return nil
}

func (handle retainedReplaceHandle) remove() error {
	state := handle.state
	if state == nil || state.private.name == "" {
		return nil
	}
	if err := removeLinuxPrivateArtifact(state); err != nil {
		return err
	}
	if err := removeLinuxPrivateDirectory(state.private); err != nil {
		return err
	}
	state.private.name = ""
	return nil
}

func (handle retainedReplaceHandle) close() error {
	state := handle.state
	if state == nil {
		return nil
	}
	var cleanupErr error
	if state.destination == "" {
		// Publication is already committed. Retry only the empty private directory
		// and deliberately suppress failure, matching the public commit boundary.
		if state.private.name != "" {
			if err := removeLinuxPrivateDirectory(state.private); err == nil {
				state.private.name = ""
			}
		}
	} else {
		cleanupErr = handle.remove()
	}
	return joinErrors(cleanupErr, syscall.Close(state.private.pinFD), syscall.Close(state.private.parentFD))
}
