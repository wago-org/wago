// Package regularfile reads bounded local metadata without following symlinks.
package regularfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

func Read(path string, limit int64) ([]byte, error) { return read(path, limit, false) }

var errSnapshotChanged = errors.New("regular file changed while opening snapshot")

// ReadAtomicSnapshot accepts replacement of the pathname after opening, while
// requiring the opened regular file to remain unchanged. Use only for records
// whose writers publish by atomic replacement. It never needs directory writes.
func ReadAtomicSnapshot(path string, limit int64) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 32; attempt++ {
		data, err := read(path, limit, true)
		lastErr = err
		if errors.Is(err, errSnapshotChanged) {
			continue
		}
		if transientSnapshotError(err) {
			// Windows can deny an open briefly while an atomic replacement runs.
			// Bound the retry budget and preserve the last error if contention persists.
			time.Sleep(10 * time.Millisecond)
			continue
		}
		return data, err
	}
	return nil, fmt.Errorf("%s: %w", path, lastErr)
}

func read(path string, limit int64, snapshot bool) ([]byte, error) {
	if limit < 0 || limit == int64(^uint64(0)>>1) {
		return nil, fmt.Errorf("invalid file byte limit")
	}
	linked, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !linked.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a stable regular file", path)
	}
	if linked.Size() > limit {
		return nil, fmt.Errorf("%s exceeds byte limit %d", path, limit)
	}
	file, err := open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if snapshot && opened.Mode().IsRegular() && !os.SameFile(opened, linked) {
		return nil, errSnapshotChanged
	}
	if !opened.Mode().IsRegular() || !os.SameFile(opened, linked) {
		return nil, fmt.Errorf("%s is not a stable regular file", path)
	}
	if opened.Size() > limit {
		return nil, fmt.Errorf("%s exceeds byte limit %d", path, limit)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds byte limit %d", path, limit)
	}
	after, statErr := file.Stat()
	if snapshot {
		if statErr != nil {
			return nil, statErr
		}
		if after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) || int64(len(data)) != after.Size() {
			return nil, fmt.Errorf("%s changed while reading", path)
		}
		return data, nil
	}
	current, linkErr := os.Lstat(path)
	if err := errors.Join(statErr, linkErr); err != nil {
		return nil, err
	}
	if !current.Mode().IsRegular() || !os.SameFile(opened, current) || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) || int64(len(data)) != after.Size() {
		return nil, fmt.Errorf("%s changed while reading", path)
	}
	return data, nil
}
