// Package modulefile bounds CLI reads of untrusted Wasm and artifact inputs.
package modulefile

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// MaxBytes is the largest module input accepted by CLI commands.
const MaxBytes int64 = 256 << 20

// Read reads path without allocating beyond MaxBytes plus one sentinel byte.
func Read(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxBytes {
		return nil, fmt.Errorf("module %q is %d bytes; CLI limit is %d bytes", path, info.Size(), MaxBytes)
	}
	readLimit := MaxBytes + 1
	if info.Mode().IsRegular() {
		// Reserve one sentinel byte so growth after Stat is detected without
		// io.ReadAll's geometric over-allocation.
		readLimit = info.Size() + 1
	}
	data := make([]byte, int(readLimit))
	n, err := io.ReadFull(file, data)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	if int64(n) > MaxBytes {
		return nil, fmt.Errorf("module %q exceeds CLI limit of %d bytes", path, MaxBytes)
	}
	if info.Mode().IsRegular() && int64(n) > info.Size() {
		return nil, fmt.Errorf("module %q changed size while being read", path)
	}
	return data[:n:n], nil
}
