//go:build unix

package wago

import (
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const compileSpoolBufferSize = 32 << 10

const maxConsecutiveEmptyCompileReads = 100

// spoolCompileInput keeps only a fixed I/O window on the Go heap. mmap makes
// the legacy byte-backed decoder usable without a second full Go allocation;
// the file is unlinked immediately and both map and descriptor are released by
// the returned function.
func spoolCompileInput(r io.Reader, limit int64) ([]byte, func(), error) {
	f, total, cleanup, err := spoolCompileInputFile(r, limit)
	if err != nil {
		return nil, nil, err
	}
	if total == 0 {
		cleanup()
		return []byte{}, func() {}, nil
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, int(total), syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("wago: map compile spool: %w", err)
	}
	return data, func() { _ = syscall.Munmap(data); cleanup() }, nil
}

// spoolCompileInputFile writes a bounded stream using a fixed I/O window but
// deliberately leaves it unmapped. CompileReader feeds this file to the
// section-stream decoder, so it never maps the whole source merely to parse it.
func spoolCompileInputFile(r io.Reader, limit int64) (*os.File, int64, func(), error) {
	if r == nil {
		return nil, 0, nil, fmt.Errorf("wago: nil compile reader")
	}
	f, err := os.CreateTemp("", "wago-compile-*.wasm")
	if err != nil {
		return nil, 0, nil, fmt.Errorf("wago: create compile spool: %w", err)
	}
	name := f.Name()
	cleanup := func() { _ = f.Close(); _ = os.Remove(name) }
	buf := make([]byte, compileSpoolBufferSize)
	var total int64
	emptyReads := 0
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			emptyReads = 0
			if int64(n) > limit-total {
				cleanup()
				return nil, 0, nil, &ResourceLimitError{Resource: "compile input", Limit: limit, Used: total + int64(n)}
			}
			if _, err := f.Write(buf[:n]); err != nil {
				cleanup()
				return nil, 0, nil, fmt.Errorf("wago: write compile spool: %w", err)
			}
			total += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			cleanup()
			return nil, 0, nil, fmt.Errorf("wago: read compile input: %w", readErr)
		}
		if n == 0 {
			emptyReads++
			if emptyReads < maxConsecutiveEmptyCompileReads {
				continue
			}
			cleanup()
			return nil, 0, nil, fmt.Errorf("wago: compile reader returned no data and no error %d times", emptyReads)
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, 0, nil, fmt.Errorf("wago: rewind compile spool: %w", err)
	}
	return f, total, cleanup, nil
}

func compileReaderWithConfig(cfg *RuntimeConfig, r Reader) (*Compiled, error) {
	f, _, release, err := spoolCompileInputFile(r, cfg.maxCompileInputBytes)
	if err != nil {
		return nil, err
	}
	defer release()
	dm, err := wasm.DecodeModuleForCompileStream(f)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	defer dm.Close()
	return compileDecodedModule(cfg, dm.Module)
}

func compileReaderAtWithConfig(cfg *RuntimeConfig, r ReaderAt, size int64) (*Compiled, error) {
	if r == nil {
		return nil, fmt.Errorf("wago: nil compile reader-at")
	}
	if size < 0 {
		return nil, fmt.Errorf("wago: negative compile reader-at size %d", size)
	}
	if size > cfg.maxCompileInputBytes {
		return nil, &ResourceLimitError{Resource: "compile input", Limit: cfg.maxCompileInputBytes, Used: size}
	}
	dm, err := wasm.DecodeModuleForCompileStream(io.NewSectionReader(r, 0, size))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	defer dm.Close()
	return compileDecodedModule(cfg, dm.Module)
}
