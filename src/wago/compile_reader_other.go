//go:build !unix

package wago

import (
	"bytes"
	"fmt"
	"io"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Non-Unix targets do not have the mmap-backed spool used by the primary
// runtime targets. Keep the same limit/error contract here; this fallback is
// intentionally isolated so it cannot affect Unix compile-memory behavior.
func spoolCompileInput(r io.Reader, limit int64) ([]byte, func(), error) {
	if r == nil {
		return nil, nil, fmt.Errorf("wago: nil compile reader")
	}
	var b bytes.Buffer
	n, err := io.Copy(&b, io.LimitReader(r, limit+1))
	if err != nil {
		return nil, nil, fmt.Errorf("wago: read compile input: %w", err)
	}
	if n > limit {
		return nil, nil, &ResourceLimitError{Resource: "compile input", Limit: limit, Used: n}
	}
	return b.Bytes(), func() {}, nil
}

func compileReaderWithConfig(cfg *RuntimeConfig, r Reader) (*Compiled, error) {
	data, release, err := spoolCompileInput(r, cfg.maxCompileInputBytes)
	if err != nil {
		return nil, err
	}
	defer release()
	m, err := wasm.DecodeModuleForCompile(data)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return compileDecodedModule(cfg, m)
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
	return compileReaderWithConfig(cfg, io.NewSectionReader(r, 0, size))
}
