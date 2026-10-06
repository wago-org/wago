//go:build darwin

package atomicfile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// InspectNewFileMetadata samples direct-child ownership and ACL inheritance on
// an empty inode. Artifact bytes are written to a distinct ACL-free private
// stage, so a reader that opened this probe can never observe the artifact.
func InspectNewFileMetadata(destination string, mode fs.FileMode, inspect func(*os.File) error) (resultErr error) {
	if inspect == nil {
		return errors.New("new-file metadata inspector is nil")
	}
	directory := filepath.Dir(destination)
	private, err := createDarwinPrivateDirectory(directory, ".wago-metadata-")
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, private.removeAndClose())
	}()

	name, fd, err := createDarwinMetadataProbe(int(private.parent.Fd()), mode)
	if err != nil {
		return fmt.Errorf("create direct-child metadata probe: %w", err)
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return errors.Join(fmt.Errorf("inspect direct-child metadata probe: %w", err), unix.Close(fd))
	}
	var source unix.Stat_t
	if err := unix.Fstatat(int(private.parent.Fd()), name, &source, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
		opened.Dev != source.Dev || opened.Ino != source.Ino || source.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.Join(fmt.Errorf("new-file metadata probe changed before isolation; recover the empty entry at %s",
			filepath.Join(directory, name)), err, unix.Close(fd))
	}
	// The probe remains empty. Pre/post identity checks bound the portable
	// compare-to-rename race, and an observed substitution is restored with EXCL
	// rather than inspected or deleted as if it were ours.
	if err := moveDarwinMetadataProbe(int(private.parent.Fd()), name, int(private.file.Fd())); err != nil {
		// Never path-delete a failed direct probe: it is empty, while its random
		// pathname could already identify a concurrent replacement.
		return errors.Join(fmt.Errorf("isolate empty metadata probe at %s: %w",
			filepath.Join(directory, name), err), unix.Close(fd))
	}
	var moved unix.Stat_t
	if err := unix.Fstatat(int(private.file.Fd()), "metadata", &moved, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
		opened.Dev != moved.Dev || opened.Ino != moved.Ino || moved.Mode&unix.S_IFMT != unix.S_IFREG {
		restoreErr := unix.RenameatxNp(int(private.file.Fd()), "metadata",
			int(private.parent.Fd()), name, unix.RENAME_EXCL)
		recovery := filepath.Join(directory, private.name, "metadata")
		if restoreErr == nil {
			recovery = filepath.Join(directory, name)
		}
		return errors.Join(fmt.Errorf("new-file metadata probe changed before inspection; recover the empty entry at %s",
			recovery), err, restoreErr, unix.Close(fd))
	}
	// Match the old O_WRONLY creation authorization and keep inspecting through
	// that already-pinned descriptor. Reopening with O_EVTONLY implicitly requests
	// data-read access on Darwin and would reject a valid write-only inherited ACL;
	// fgetattrlist instead authorizes its requested metadata independently of the
	// descriptor's data-access mode. A restrictive umask can leave the empty probe
	// mode 000, so grant only its owner access after it is isolated in the ACL-free
	// private directory. Darwin chmod changes mode bits without rewriting the
	// inherited ACL that this probe exists to sample; the probe is deleted without
	// ever carrying artifact bytes.
	if opened.Mode&0o600 != 0o600 {
		if err := unix.Fchmod(fd, 0o600); err != nil {
			return errors.Join(fmt.Errorf("make isolated metadata probe inspectable: %w", err),
				unix.Close(fd), unix.Unlinkat(int(private.file.Fd()), "metadata", 0))
		}
	}
	file := os.NewFile(uintptr(fd), filepath.Join(directory, private.name, "metadata"))
	if file == nil {
		_ = unix.Close(fd)
		return errors.Join(errors.New("wrap new-file metadata probe"),
			unix.Unlinkat(int(private.file.Fd()), "metadata", 0))
	}
	inspectErr := inspect(file)
	if inspectErr != nil {
		inspectErr = fmt.Errorf("inspect isolated metadata probe: %w", inspectErr)
	}
	unlinkErr := unix.Unlinkat(int(private.file.Fd()), "metadata", 0)
	if unlinkErr != nil {
		unlinkErr = fmt.Errorf("remove isolated metadata probe: %w", unlinkErr)
	}
	closeErr := file.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close isolated metadata probe: %w", closeErr)
	}
	return errors.Join(inspectErr, unlinkErr, closeErr)
}

func moveDarwinMetadataProbe(parentFD int, name string, privateFD int) error {
	err := unix.RenameatxNp(parentFD, name, privateFD, "metadata", unix.RENAME_EXCL)
	if err == nil || (!errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.ENOTSUP) &&
		!errors.Is(err, unix.EOPNOTSUPP) && !errors.Is(err, unix.ENOSYS)) {
		return err
	}
	// The protected destination is freshly empty. Restoration remains EXCL-only
	// because the public random name may have been recreated concurrently.
	return unix.Renameat(parentFD, name, privateFD, "metadata")
}

func createDarwinMetadataProbe(parentFD int, mode fs.FileMode) (string, int, error) {
	for attempts := 0; attempts < 100; attempts++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", -1, err
		}
		name := ".wago-metadata-" + hex.EncodeToString(random[:])
		fd, err := unix.Openat(parentFD, name,
			unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		return name, fd, err
	}
	return "", -1, fmt.Errorf("create new-file metadata probe: too many name collisions")
}
