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
	Mode  fs.FileMode
	Sync  bool
	Hooks *Hooks
}

// Hooks supports deterministic failure testing at pre-commit boundaries.
// Production callers must leave Hooks nil.
type Hooks struct {
	Sync    func(*os.File) error
	Close   func(*os.File) error
	Replace func(source, destination string) error
}

// ReplaceFile writes a unique restrictive temporary file in the destination
// directory, finalizes it, and atomically replaces destination. Existing
// directories, symlinks, and non-regular files are rejected.
func ReplaceFile(destination string, options Options, write func(io.Writer) error) error {
	if write == nil {
		return errors.New("atomic file writer is nil")
	}
	if err := validateDestination(destination); err != nil {
		return err
	}
	file, err := createTemp(destination)
	if err != nil {
		return err
	}
	temporary := file.Name()
	closed := false
	committed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if err := write(file); err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := finalize(file, options); err != nil {
		closed = true
		return err
	}
	closed = true
	if err := validateDestination(destination); err != nil {
		return err
	}
	if err := replace(options, temporary, destination); err != nil {
		return fmt.Errorf("replace %s: %w", destination, err)
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
	defer func() {
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if filepath.Clean(filepath.Dir(temporary)) != filepath.Clean(filepath.Dir(destination)) {
		return errors.New("atomic temporary file must be in the destination directory")
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
	if err := replace(options, temporary, destination); err != nil {
		return fmt.Errorf("replace %s: %w", destination, err)
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
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	return os.CreateTemp(directory, ".wago-atomic-*")
}

func finalize(file *os.File, options Options) error {
	mode := options.Mode.Perm()
	if mode == 0 {
		mode = 0o600
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return fmt.Errorf("set temporary file mode: %w", err)
	}
	if options.Sync {
		syncFile := (*os.File).Sync
		if options.Hooks != nil && options.Hooks.Sync != nil {
			syncFile = options.Hooks.Sync
		}
		if err := syncFile(file); err != nil {
			_ = file.Close()
			return fmt.Errorf("sync temporary file: %w", err)
		}
	}
	if options.Hooks != nil && options.Hooks.Close != nil {
		if err := options.Hooks.Close(file); err != nil {
			// A failure injector must not be able to leave the descriptor open.
			// This matters on Windows, where an open temporary file cannot be
			// removed or moved reliably during cleanup.
			_ = file.Close()
			return fmt.Errorf("close temporary file: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	return nil
}

func replace(options Options, source, destination string) error {
	if options.Hooks != nil && options.Hooks.Replace != nil {
		return options.Hooks.Replace(source, destination)
	}
	return replaceExisting(source, destination)
}

func validateDestination(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect destination %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("destination %s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("destination %s is not a regular file", path)
	}
	return nil
}

func validateRegular(path, label string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s %s is not a regular file", label, path)
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
		return fmt.Errorf("temporary file %s changed before publication", path)
	}
	return nil
}
