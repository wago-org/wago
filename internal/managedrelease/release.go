// Package managedrelease publishes immutable manager/source pairs through one
// atomic selection record. Old releases remain available for running processes
// and rollback. Publication prunes inactive releases beyond current/previous.
package managedrelease

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/filelock"
	"github.com/wago-org/wago/internal/jsonstrict"
	"github.com/wago-org/wago/internal/regularfile"
)

const pointerFile = ".wago-release.json"
const releasesDir = ".wago-releases"
const publicationLockFile = ".wago-release.lock"

// PublicationLockPath is the shared coordinator for preparation, publication,
// and uninstall. Uninstall retires it only after destructive cleanup is done.
func PublicationLockPath(executable string) string {
	return filepath.Join(filepath.Dir(Launcher(executable)), publicationLockFile)
}

type Record struct {
	Format   int    `json:"format"`
	Release  string `json:"release"`
	Previous string `json:"previous,omitempty"`
}
type Release struct {
	Root      string
	Directory string
	Version   string
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "wago.exe"
	}
	return "wago"
}
func (r *Release) Binary() string { return filepath.Join(r.Directory, executableName()) }
func (r *Release) Source() string { return filepath.Join(r.Directory, "src") }

func Prepare(launcher, version string, write func(binary, source string) error, verify func(binary string) error) (*Release, error) {
	root := filepath.Dir(launcher)
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	lock, err := lockForPublication(root)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := os.MkdirAll(filepath.Join(root, releasesDir), 0755); err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp(filepath.Join(root, releasesDir), "release-")
	if err != nil {
		return nil, err
	}
	r := &Release{Root: root, Directory: directory, Version: version}
	if err := os.WriteFile(filepath.Join(directory, leaseFile), nil, 0644); err != nil {
		os.RemoveAll(directory)
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(directory)
		}
	}()
	if err := write(r.Binary(), r.Source()); err != nil {
		return nil, err
	}
	if _, err := regularfile.Read(filepath.Join(r.Source(), "go.mod"), 1<<20); err != nil {
		return nil, fmt.Errorf("release source: %w", err)
	}
	if err := verify(r.Binary()); err != nil {
		return nil, fmt.Errorf("verify staged release: %w", err)
	}
	if err := syncTree(directory); err != nil {
		return nil, err
	}
	// The receipt binds source discovery to the immutable executable location.
	if err := atomicfile.ReplaceFile(filepath.Join(directory, "release.json"), atomicfile.Options{Mode: 0644, Sync: true}, func(w io.Writer) error {
		return json.NewEncoder(w).Encode(struct {
			Version string `json:"version"`
		}{version})
	}); err != nil {
		return nil, err
	}
	if err := syncDirectory(directory); err != nil {
		return nil, err
	}
	if err := syncDirectory(filepath.Dir(directory)); err != nil {
		return nil, err
	}
	success = true
	return r, nil
}

func readRecord(root string) (Record, []byte, error) {
	data, err := regularfile.ReadAtomicSnapshot(filepath.Join(root, pointerFile), 4096)
	if err != nil {
		return Record{}, nil, err
	}
	var record Record
	if err := jsonstrict.ValidateTypedJSON(data, record); err != nil {
		return record, nil, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&record); err != nil {
		return record, nil, err
	}
	valid := func(name string) bool {
		return strings.HasPrefix(name, "release-") && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\")
	}
	if record.Format != 1 || !valid(record.Release) || (record.Previous != "" && !valid(record.Previous)) {
		return record, nil, fmt.Errorf("invalid manager release record")
	}
	return record, data, nil
}

// Publish keeps the old selection until the pair has passed verification. The
// optional bootstrap installs a dispatcher for legacy/first installations. It
// returns an undo function for any side effects, including when it returns an
// error. Both run under the publication lock; pre-commit failures restore the
// selection and undo bootstrap. Marker-directory sync and pruning follow commit.
func Publish(r *Release, bootstrap func() (func() error, error), hooks *atomicfile.Hooks) error {
	lock, err := lockForPublication(r.Root)
	if err != nil {
		return err
	}
	defer lock.Close()
	lease, err := filelock.TryAcquireSharedExisting(filepath.Join(r.Directory, leaseFile))
	if err != nil {
		return fmt.Errorf("pin release for publication: %w", err)
	}
	if lease == nil {
		return fmt.Errorf("release is being retired")
	}
	defer lease.Close()
	old, oldData, err := readRecord(r.Root)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	record := Record{Format: 1, Release: filepath.Base(r.Directory), Previous: old.Release}
	if old.Release == record.Release {
		record.Previous = old.Previous
	}
	path := filepath.Join(r.Root, pointerFile)
	if err := atomicfile.ReplaceFile(path, atomicfile.Options{Mode: 0644, Sync: true, Hooks: hooks}, func(w io.Writer) error { return json.NewEncoder(w).Encode(record) }); err != nil {
		return err
	}
	var undoBootstrap func() error
	rollbackSelection := func(cause error) error {
		var rollback error
		if oldData == nil {
			rollback = os.Remove(path)
		} else {
			rollback = atomicfile.ReplaceFile(path, atomicfile.Options{Mode: 0644, Sync: true}, func(w io.Writer) error { _, e := w.Write(oldData); return e })
		}
		if rollback != nil {
			cause = errors.Join(cause, fmt.Errorf("restore release pointer (staged pair retained at %s): %w", r.Directory, rollback))
		}
		if undoBootstrap != nil {
			if err := undoBootstrap(); err != nil {
				cause = errors.Join(cause, fmt.Errorf("restore launcher (staged pair retained at %s): %w", r.Directory, err))
			}
		}
		return errors.Join(cause, syncDirectory(r.Root))
	}
	if bootstrap != nil {
		undoBootstrap, err = bootstrap()
		if err != nil {
			return rollbackSelection(err)
		}
	}
	if err := syncDirectory(r.Root); err != nil {
		return rollbackSelection(err)
	}
	if err := atomicfile.ReplaceFile(filepath.Join(r.Directory, publishedFile), atomicfile.Options{Mode: 0644, Sync: true}, func(w io.Writer) error { return nil }); err != nil {
		return rollbackSelection(err)
	}
	if err := syncDirectory(r.Directory); err != nil {
		return fmt.Errorf("manager selected, but release marker sync failed: %w", err)
	}
	if err := pruneReleases(r.Root, record); err != nil {
		return fmt.Errorf("manager selected, but old release cleanup failed: %w", err)
	}
	return nil
}

// BootstrapLauncher replaces the stable executable and returns a rollback
// action. Publish must call it while holding the publication lock. The backup
// remains inside the staged pair if any later rollback step fails.
func BootstrapLauncher(r *Release, source, destination string) (func() error, error) {
	backup := filepath.Join(r.Directory, "previous-launcher")
	previous, err := os.Stat(destination)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if previous != nil {
		if err := CopyFile(destination, backup); err != nil {
			return nil, err
		}
	}
	if err := CopyFile(source, destination); err != nil {
		return nil, err // Atomic replacement did not change the launcher.
	}
	return func() error {
		if previous == nil {
			if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove new launcher %s: %w", destination, err)
			}
			return nil
		}
		if err := copyFileMode(backup, destination, previous.Mode().Perm()); err != nil {
			return fmt.Errorf("restore %s from %s: %w", destination, backup, err)
		}
		return nil
	}, nil
}

func SelectedBinary(launcher string) (string, error) {
	record, _, err := readRecord(filepath.Dir(launcher))
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(launcher), releasesDir, record.Release, executableName()), nil
}

// SourceForExecutable uses the executable's own release, never the current
// selection, so a running old manager keeps its matching source after updates.
func SourceForExecutable(executable string) string {
	directory := filepath.Dir(executable)
	if filepath.Base(filepath.Dir(directory)) != releasesDir {
		return ""
	}
	if _, err := regularfile.Read(filepath.Join(directory, "release.json"), 4096); err != nil {
		return ""
	}
	return filepath.Join(directory, "src")
}

// ExecutablePath resolves aliases before locating sibling release metadata.
// The caller-controlled argv[0] is never used to identify the executable.
func ExecutablePath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(executable)
}

func Source() string {
	executable, err := ExecutablePath()
	if err != nil {
		return ""
	}
	return SourceForExecutable(executable)
}
func Launcher(executable string) string {
	if SourceForExecutable(executable) != "" {
		return filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(executable))), executableName())
	}
	return executable
}

// RemovalTargets lists only the manager's reserved release artifacts. It works
// for either the stable launcher or a payload and does not include sibling files.
func RemovalTargets(executable string) []string {
	var targets []string
	for _, path := range CleanupPaths(executable) {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			targets = append(targets, path)
		}
	}
	return targets
}

// CleanupPaths includes all manager state, even if it is absent while cleanup
// is scheduled, so partial releases and a deferred cleanup marker are included.
func CleanupPaths(executable string) []string {
	root := filepath.Dir(Launcher(executable))
	return []string{filepath.Join(root, pointerFile), filepath.Join(root, publicationLockFile), filepath.Join(root, releasesDir), filepath.Join(root, uninstallPendingFile)}
}

// CopyFile makes a durable executable copy without replacing the source.
func CopyFile(source, destination string) error {
	return copyFileMode(source, destination, 0755)
}

func copyFileMode(source, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	return atomicfile.ReplaceFile(destination, atomicfile.Options{Mode: mode, Sync: true}, func(w io.Writer) error { _, err := io.Copy(w, input); return err })
}

func syncTree(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("release contains symlink %s", path)
		}
		file, err := openForSync(path)
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err == nil && !info.Mode().IsRegular() {
			err = fmt.Errorf("release contains non-regular file %s", path)
		}
		if err == nil {
			err = file.Sync()
		}
		return errors.Join(err, file.Close())
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := syncDirectory(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}
