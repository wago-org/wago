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

// Read reads path without accepting more than MaxBytes.
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
	if info.Mode().IsRegular() {
		// Reserve one sentinel byte so growth after Stat is detected without
		// io.ReadAll's geometric over-allocation.
		readLimit := info.Size() + 1
		data := make([]byte, int(readLimit))
		n, err := io.ReadFull(file, data)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, err
		}
		if int64(n) > MaxBytes {
			return nil, fmt.Errorf("module %q exceeds CLI limit of %d bytes", path, MaxBytes)
		}
		if int64(n) > info.Size() {
			return nil, fmt.Errorf("module %q changed size while being read", path)
		}
		return data[:n:n], nil
	}
	return readStream(path, file)
}

// readStream spools pipes and other unknown-length inputs through a bounded
// fixed-size buffer. This avoids reserving the entire input limit before the
// stream has produced any bytes while still returning one exact-sized slice.
func readStream(path string, source io.Reader) ([]byte, error) {
	temporary, err := os.CreateTemp("", "wago-module-*")
	if err != nil {
		return nil, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	defer temporary.Close()

	buffer := make([]byte, 64<<10)
	n, err := io.CopyBuffer(temporary, io.LimitReader(source, MaxBytes+1), buffer)
	if err != nil {
		return nil, err
	}
	if n > MaxBytes {
		return nil, fmt.Errorf("module %q exceeds CLI limit of %d bytes", path, MaxBytes)
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data := make([]byte, int(n))
	read, err := io.ReadFull(temporary, data)
	if err != nil {
		return nil, err
	}
	return data[:read:read], nil
}
