// Package modulefile bounds CLI reads of untrusted Wasm and artifact inputs.
package modulefile

import (
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
	if info, err := file.Stat(); err != nil {
		return nil, err
	} else if info.Size() > MaxBytes {
		return nil, fmt.Errorf("module %q is %d bytes; CLI limit is %d bytes", path, info.Size(), MaxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxBytes {
		return nil, fmt.Errorf("module %q exceeds CLI limit of %d bytes", path, MaxBytes)
	}
	return data, nil
}
