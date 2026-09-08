package project

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wago-org/wago/internal/jsonstrict"
	"github.com/wago-org/wago/internal/regularfile"
	"io"
	"os"
	"path/filepath"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/filelock"
)

const (
	projectTransactionFormat = 1
	projectDirectory         = ".wago"
	projectLockFile          = "project.lock"
	projectJournalFile       = "project-transaction.json"
)

var replaceProjectFile = atomicfile.ReplaceFile

// Mutation owns the project-wide metadata lock. It is valid only during the
// callback passed to WithMutation.
type Mutation struct {
	dir    string
	active bool
}

type committedTransactionError struct{ err error }

func (err *committedTransactionError) Error() string { return err.err.Error() }
func (err *committedTransactionError) Unwrap() error { return err.err }

// TransactionCommitted reports whether the requested metadata is durably
// recorded in the recovery journal and must be completed rather than rolled
// back by a wider caller transaction.
func TransactionCommitted(err error) bool {
	var committed *committedTransactionError
	return errors.As(err, &committed)
}

type projectJournal struct {
	FormatVersion int          `json:"formatVersion"`
	Manifest      *journalFile `json:"manifest,omitempty"`
	Lock          *journalFile `json:"lock,omitempty"`
}

type journalFile struct {
	Data   []byte `json:"data"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

func projectLockPath(dir string) string {
	return filepath.Join(dir, projectDirectory, projectLockFile)
}

func projectJournalPath(dir string) string {
	return filepath.Join(dir, projectDirectory, projectJournalFile)
}

// withMetadataRead avoids creating project state for ordinary read-only
// access. Once a writer has created the shared lock, readers join that lock and
// recover any committed journal before inspecting metadata.
func withMetadataRead(dir string, fn func(*Mutation) error) error {
	for _, path := range []string{projectJournalPath(dir), projectLockPath(dir)} {
		if _, err := os.Lstat(path); err == nil {
			return WithMutation(context.Background(), dir, fn)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	mutation := &Mutation{dir: dir, active: true}
	defer func() { mutation.active = false }()
	return fn(mutation)
}

// WithMutation serializes project metadata access across processes, recovers
// any committed interrupted update, and then calls fn while holding the lock.
func WithMutation(ctx context.Context, dir string, fn func(*Mutation) error) (err error) {
	if fn == nil {
		return errors.New("project mutation callback is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	lock, err := filelock.Acquire(ctx, projectLockPath(dir))
	if err != nil {
		return fmt.Errorf("lock project metadata: %w", err)
	}
	mutation := &Mutation{dir: dir, active: true}
	defer func() {
		mutation.active = false
		err = errors.Join(err, lock.Close())
	}()
	if err := mutation.recover(); err != nil {
		return err
	}
	return fn(mutation)
}

// ReadManifest reads the current manifest while the project lock is held.
func (mutation *Mutation) ReadManifest() (map[string]any, error) {
	if err := mutation.ensureActive(); err != nil {
		return nil, err
	}
	return readManifest(mutation.dir)
}

// ReadLock reads the current lock document while the project lock is held.
func (mutation *Mutation) ReadLock() (LockDocument, error) {
	if err := mutation.ensureActive(); err != nil {
		return LockDocument{}, err
	}
	return readLock(mutation.dir)
}

// PublishManifest validates and atomically publishes one manifest through the
// same crash-recoverable transaction used for paired metadata updates.
func (mutation *Mutation) PublishManifest(manifest map[string]any) error {
	data, err := EncodeManifest(manifest)
	if err != nil {
		return err
	}
	return mutation.publish(&data, nil)
}

// PublishLock validates and atomically publishes one lock document through the
// same crash-recoverable transaction used for paired metadata updates.
func (mutation *Mutation) PublishLock(document LockDocument) error {
	data, err := EncodeLock(document)
	if err != nil {
		return err
	}
	return mutation.publish(nil, &data)
}

// PublishMetadata atomically publishes a coherent manifest and lock document.
func (mutation *Mutation) PublishMetadata(manifest map[string]any, document LockDocument) error {
	manifestData, err := EncodeManifest(manifest)
	if err != nil {
		return err
	}
	lockData, err := EncodeLock(document)
	if err != nil {
		return err
	}
	requirements, err := requirementsFromMap(manifest, mutation.dir)
	if err != nil {
		return err
	}
	if err := ValidateLockedResolution(requirements, document); err != nil {
		return fmt.Errorf("manifest and lock are inconsistent: %w", err)
	}
	return mutation.publish(&manifestData, &lockData)
}

// PublishEncodedMetadata is for callers that already staged and validated the
// exact bytes used to build another project artifact. It decodes and validates
// both documents again before publication.
func (mutation *Mutation) PublishEncodedMetadata(manifestData, lockData []byte) error {
	manifest, err := decodeManifest(manifestData, mutation.dir)
	if err != nil {
		return err
	}
	document, err := DecodeLock(lockData)
	if err != nil {
		return err
	}
	requirements, err := requirementsFromMap(manifest, mutation.dir)
	if err != nil {
		return err
	}
	if err := ValidateLockedResolution(requirements, document); err != nil {
		return fmt.Errorf("manifest and lock are inconsistent: %w", err)
	}
	manifestCopy, lockCopy := append([]byte(nil), manifestData...), append([]byte(nil), lockData...)
	return mutation.publish(&manifestCopy, &lockCopy)
}

func (mutation *Mutation) publish(manifestData, lockData *[]byte) error {
	if err := mutation.ensureActive(); err != nil {
		return err
	}
	if automation.Locked() {
		return fmt.Errorf("locked mode prevents changing project metadata")
	}
	var err error
	journal := projectJournal{FormatVersion: projectTransactionFormat}
	if manifestData != nil {
		journal.Manifest, err = newJournalFile(Path(mutation.dir), *manifestData)
		if err != nil {
			return err
		}
	}
	if lockData != nil {
		journal.Lock, err = newJournalFile(LockPath(mutation.dir), *lockData)
		if err != nil {
			return err
		}
	}
	if journal.Manifest == nil && journal.Lock == nil {
		return errors.New("project transaction has no files")
	}
	encoded, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if len(encoded) > maxProjectJournalBytes {
		return fmt.Errorf("project journal exceeds byte limit %d", maxProjectJournalBytes)
	}
	journalPath := projectJournalPath(mutation.dir)
	if err := replaceProjectFile(journalPath, atomicfile.Options{Mode: 0o600, Sync: true}, func(writer io.Writer) error {
		_, err := writer.Write(encoded)
		return err
	}); err != nil {
		return fmt.Errorf("commit project transaction journal: %w", err)
	}
	if err := syncProjectDirectory(filepath.Dir(journalPath)); err != nil {
		return &committedTransactionError{err: fmt.Errorf("sync project transaction journal: %w", err)}
	}
	if err := mutation.applyJournal(journal); err != nil {
		return &committedTransactionError{err: err}
	}
	return nil
}

func (mutation *Mutation) recover() error {
	path := projectJournalPath(mutation.dir)
	data, err := regularfile.Read(path, maxProjectJournalBytes)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read project transaction journal: %w", err)
	}
	if err := jsonstrict.ValidateTypedJSONWithLimits(data, projectJournal{}, projectJSONLimits); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var journal projectJournal
	if err := decoder.Decode(&journal); err != nil {
		return fmt.Errorf("decode project transaction journal: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return fmt.Errorf("decode project transaction journal: %w", err)
	}
	if err := validateJournal(journal); err != nil {
		return err
	}
	return mutation.applyJournal(journal)
}

func (mutation *Mutation) applyJournal(journal projectJournal) error {
	if journal.Manifest != nil {
		if err := writeJournalFile(Path(mutation.dir), journal.Manifest); err != nil {
			return fmt.Errorf("recover %s: %w", File, err)
		}
	}
	if journal.Lock != nil {
		if err := writeJournalFile(LockPath(mutation.dir), journal.Lock); err != nil {
			return fmt.Errorf("recover %s: %w", LockFile, err)
		}
	}
	if err := syncProjectDirectory(mutation.dir); err != nil {
		return fmt.Errorf("sync project metadata: %w", err)
	}
	journalPath := projectJournalPath(mutation.dir)
	if err := os.Remove(journalPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove project transaction journal: %w", err)
	}
	if err := syncProjectDirectory(filepath.Dir(journalPath)); err != nil {
		return fmt.Errorf("sync project transaction completion: %w", err)
	}
	return nil
}

func writeJournalFile(path string, file *journalFile) error {
	return replaceProjectFile(path, atomicfile.Options{Mode: os.FileMode(file.Mode), Sync: true}, func(writer io.Writer) error {
		_, err := writer.Write(file.Data)
		return err
	})
}

func newJournalFile(path string, data []byte) (*journalFile, error) {
	if len(data) > maxProjectMetadataBytes {
		return nil, fmt.Errorf("project metadata exceeds byte limit %d", maxProjectMetadataBytes)
	}
	mode := os.FileMode(0o644)
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("project metadata target %s is not a regular file", path)
		}
		mode = info.Mode().Perm()
		if mode == 0 {
			return nil, fmt.Errorf("project metadata target %s has unsupported mode 0000", path)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	sum := sha256.Sum256(data)
	return &journalFile{Data: append([]byte(nil), data...), SHA256: hex.EncodeToString(sum[:]), Mode: uint32(mode)}, nil
}

func validateJournal(journal projectJournal) error {
	if journal.FormatVersion != projectTransactionFormat {
		return fmt.Errorf("unsupported project transaction format %d", journal.FormatVersion)
	}
	if journal.Manifest == nil && journal.Lock == nil {
		return errors.New("project transaction journal has no files")
	}
	for name, file := range map[string]*journalFile{File: journal.Manifest, LockFile: journal.Lock} {
		if file == nil {
			continue
		}
		sum := sha256.Sum256(file.Data)
		if file.SHA256 != hex.EncodeToString(sum[:]) {
			return fmt.Errorf("project transaction journal has invalid %s checksum", name)
		}
		if file.Mode == 0 || file.Mode > 0o777 {
			return fmt.Errorf("project transaction journal has invalid %s mode %04o", name, file.Mode)
		}
	}
	var manifest map[string]any
	var document LockDocument
	if journal.Manifest != nil {
		var err error
		if manifest, err = decodeManifest(journal.Manifest.Data, "."); err != nil {
			return fmt.Errorf("project transaction journal has invalid %s: %w", File, err)
		}
	}
	if journal.Lock != nil {
		var err error
		if document, err = DecodeLock(journal.Lock.Data); err != nil {
			return fmt.Errorf("project transaction journal has invalid %s: %w", LockFile, err)
		}
	}
	if journal.Manifest != nil && journal.Lock != nil {
		requirements, err := requirementsFromMap(manifest, ".")
		if err != nil {
			return fmt.Errorf("project transaction journal has invalid %s: %w", File, err)
		}
		if err := ValidateLockedResolution(requirements, document); err != nil {
			return fmt.Errorf("project transaction journal metadata is inconsistent: %w", err)
		}
	}
	return nil
}

func (mutation *Mutation) ensureActive() error {
	if mutation == nil || !mutation.active {
		return errors.New("project mutation is not active")
	}
	return nil
}
