// Package atomicfile publishes complete files through unique same-directory
// temporary files and platform-correct replace-existing operations.
package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Options controls construction and publication. Sync flushes file contents
// before the atomic replacement; it does not promise parent-directory durability.
type Options struct {
	Mode fs.FileMode
	// ModeSet distinguishes an intentional mode 000 from an omitted Mode.
	// Non-zero Mode values remain explicit for compatibility with existing callers.
	ModeSet bool
	// ApplyUmask derives the final Mode through the kernel's process umask while
	// deferring that mode until the artifact-bearing temporary file is finalized.
	ApplyUmask bool
	Sync       bool
	// RequireExistingParent prevents ReplaceFile from creating a missing parent.
	// The default remains parent creation for compatibility with existing callers.
	RequireExistingParent bool
	// BeforeReplace validates caller-specific state after the temporary file is
	// finalized and destination type is checked, immediately before replacement.
	BeforeReplace func(destination string) error
	// RetainReplaceHandle asks ReplaceFile to reserve platform-specific staging,
	// publication, and cleanup authority when the temporary file is created. Build
	// uses it for Windows DACL changes and Linux/Darwin private artifact staging.
	// Linux isolates a mode-zero direct child before writing bytes; Darwin samples
	// missing-output inheritance with a separate empty probe.
	RetainReplaceHandle bool
	Hooks               *Hooks
}

// Hooks supports deterministic failure testing at pre-commit boundaries.
// Production callers must leave Hooks nil.
type Hooks struct {
	Sync    func(*os.File) error
	Close   func(*os.File) error
	Replace func(source, destination string) error
}

// These helpers centralize cold failure construction. Atomic publication has
// many deliberate checks; preventing compiler expansion at each call site keeps
// their linked-code cost bounded without allocating on successful builds.
//
//go:noinline
func joinErrors(values ...error) error { return errors.Join(values...) }

// Typed helpers share interface-slice construction across cold failure sites.
//
//go:noinline
func formatErrorE(format string, err error) error { return fmt.Errorf(format, err) }

//go:noinline
func formatErrorS(format, value string) error { return fmt.Errorf(format, value) }

//go:noinline
func formatErrorSE(format, value string, err error) error { return fmt.Errorf(format, value, err) }

//go:noinline
func formatErrorSS(format, first, second string) error { return fmt.Errorf(format, first, second) }

//go:noinline
func newError(message string) error {
	return errors.New(message)
}

// ReplaceFile writes a unique restrictive temporary file in the destination
// directory, finalizes it, and atomically replaces destination. Existing
// directories, symlinks, and non-regular files are rejected.
func ReplaceFile(destination string, options Options, write func(io.Writer) error) (resultErr error) {
	if write == nil {
		return newError("atomic file writer is nil")
	}
	if err := validateDestination(destination); err != nil {
		return err
	}
	file, finalizeOptions, reservation, err := createReplacementTemp(destination, options)
	if err != nil {
		return err
	}
	temporary := file.Name()
	closed := false
	committed := false
	defer func() {
		var closeFileErr, cleanupErr error
		if !closed {
			closeFileErr = file.Close()
		}
		if !committed {
			if reservation.valid() {
				cleanupErr = reservation.remove()
			} else {
				cleanupErr = os.Remove(temporary)
			}
		}
		closeReservationErr := reservation.close()
		if closeFileErr != nil {
			resultErr = joinErrors(resultErr, formatErrorE("close temporary file during cleanup: %w", closeFileErr))
		}
		if cleanupErr != nil {
			resultErr = joinErrors(resultErr, formatErrorE("remove temporary file during cleanup: %w", cleanupErr))
		}
		// Retained handles carry only publication and cleanup authority; the
		// artifact writer was already finalized and closed before replacement.
		// Once replacement commits, a close failure cannot invalidate the visible
		// artifact and must not turn successful publication into a false failure.
		if closeReservationErr != nil && !committed {
			resultErr = joinErrors(resultErr, formatErrorE("close retained replacement handle: %w", closeReservationErr))
		}
	}()
	if err := write(file); err != nil {
		return formatErrorE("write temporary file: %w", err)
	}
	if err := finalize(file, finalizeOptions); err != nil {
		closed = true
		return err
	}
	closed = true
	if err := validateDestination(destination); err != nil {
		return err
	}
	if err := validateBeforeReplace(options, destination); err != nil {
		return err
	}
	if err := replace(options, reservation, temporary, destination); err != nil {
		return formatErrorSE("replace %s: %w", destination, err)
	}
	committed = true
	return nil
}

// CreateTemp creates a unique 0600 temporary file beside destination. It is
// intended for tools such as the Go compiler that require an output pathname.
// The caller must close it and either pass its name to CommitTempFile or remove it.
func CreateTemp(destination string) (*os.File, error) {
	if err := validateDestination(destination); err != nil {
		return nil, err
	}
	return createTemp(destination)
}

// CommitTempFile finalizes an existing same-directory regular temporary file
// and atomically replaces destination. The temporary file is removed on every
// pre-commit failure.
func CommitTempFile(temporary, destination string, options Options) error {
	committed := false
	// Install cleanup before validating options because the caller transfers
	// ownership of the staged pathname even when publication is rejected.
	defer func() {
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if options.ApplyUmask {
		return newError("atomic file ApplyUmask requires ReplaceFile")
	}
	if options.RetainReplaceHandle {
		return newError("atomic file RetainReplaceHandle requires ReplaceFile")
	}
	if filepath.Clean(filepath.Dir(temporary)) != filepath.Clean(filepath.Dir(destination)) {
		return newError("atomic temporary file must be in the destination directory")
	}
	if err := validateDestination(destination); err != nil {
		return err
	}
	if err := validateRegular(temporary, "temporary file"); err != nil {
		return err
	}
	file, err := os.OpenFile(temporary, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := validateOpenFile(file, temporary); err != nil {
		_ = file.Close()
		return err
	}
	if err := finalize(file, options); err != nil {
		return err
	}
	if err := validateDestination(destination); err != nil {
		return err
	}
	if err := validateBeforeReplace(options, destination); err != nil {
		return err
	}
	if err := replace(options, retainedReplaceHandle{}, temporary, destination); err != nil {
		return formatErrorSE("replace %s: %w", destination, err)
	}
	committed = true
	return nil
}

// ReplaceExisting performs the platform replacement operation. Callers that
// need temp creation, type checks, cleanup, permissions, or syncing should use
// ReplaceFile or CommitTempFile instead.
func ReplaceExisting(source, destination string) error {
	return replaceExisting(source, destination)
}

func createTemp(destination string) (*os.File, error) {
	return createTempWithParentPolicy(destination, false)
}

func createTempWithParentPolicy(destination string, requireExistingParent bool) (*os.File, error) {
	directory := filepath.Dir(destination)
	if !requireExistingParent {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, err
		}
	}
	return os.CreateTemp(directory, ".wago-atomic-*")
}

func finalize(file *os.File, options Options) error {
	if !options.ApplyUmask {
		mode := options.Mode.Perm()
		if !options.ModeSet && mode == 0 {
			mode = 0o600
		}
		if err := file.Chmod(mode); err != nil {
			_ = file.Close()
			return formatErrorE("set temporary file mode: %w", err)
		}
	}
	if options.Sync {
		syncFile := (*os.File).Sync
		if options.Hooks != nil && options.Hooks.Sync != nil {
			syncFile = options.Hooks.Sync
		}
		if err := syncFile(file); err != nil {
			_ = file.Close()
			return formatErrorE("sync temporary file: %w", err)
		}
	}
	if options.Hooks != nil && options.Hooks.Close != nil {
		if err := options.Hooks.Close(file); err != nil {
			// A failure injector must not be able to leave the descriptor open.
			// This matters on Windows, where an open temporary file cannot be
			// removed or moved reliably during cleanup.
			_ = file.Close()
			return formatErrorE("close temporary file: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		return formatErrorE("close temporary file: %w", err)
	}
	return nil
}

func createReplacementTemp(destination string, options Options) (*os.File, Options, retainedReplaceHandle, error) {
	if !options.ApplyUmask {
		file, reservation, err := createReplacementTempWithOptions(destination, options)
		return file, options, reservation, err
	}
	if !options.ModeSet && options.Mode.Perm() == 0 {
		return nil, options, retainedReplaceHandle{}, newError("atomic file ApplyUmask requires an explicit mode")
	}
	mode, err := probeUmaskMode(destination, options.Mode.Perm(), options.RequireExistingParent)
	if err != nil {
		return nil, options, retainedReplaceHandle{}, err
	}
	file, reservation, err := createReplacementTempWithOptions(destination, options)
	if err != nil {
		return nil, options, retainedReplaceHandle{}, err
	}
	// The empty probe obtains the process-umask result without exposing the real
	// artifact temp. Restore that mode only after its complete contents are ready.
	options.Mode, options.ModeSet, options.ApplyUmask = mode, true, false
	return file, options, reservation, nil
}

func createReplacementTempWithOptions(destination string, options Options) (*os.File, retainedReplaceHandle, error) {
	if options.RetainReplaceHandle {
		return createRetainedReplacementTemp(destination, options.RequireExistingParent)
	}
	file, err := createTempWithParentPolicy(destination, options.RequireExistingParent)
	return file, retainedReplaceHandle{}, err
}

func validateBeforeReplace(options Options, destination string) error {
	if options.BeforeReplace == nil {
		return nil
	}
	if err := options.BeforeReplace(destination); err != nil {
		return formatErrorE("validate destination before replace: %w", err)
	}
	return nil
}

func replace(options Options, reservation retainedReplaceHandle, source, destination string) error {
	if options.Hooks != nil && options.Hooks.Replace != nil {
		return options.Hooks.Replace(source, destination)
	}
	if reservation.valid() {
		return reservation.replace(destination)
	}
	return replaceExisting(source, destination)
}

func validateDestination(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return formatErrorSE("inspect destination %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return formatErrorS("destination %s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return formatErrorS("destination %s is not a regular file", path)
	}
	return nil
}

func validateRegular(path, label string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return formatErrorSS("%s %s is not a regular file", label, path)
	}
	return nil
}

func validateOpenFile(file *os.File, path string) error {
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	linked, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || linked.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, linked) {
		return formatErrorS("temporary file %s changed before publication", path)
	}
	return nil
}
