// Package spectest contains shared discovery helpers for the external
// WebAssembly specification corpora used by compiler and runtime tests.
package spectest

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Release2Suite identifies the official WebAssembly 2.0 core corpus within a
// checkout of the WebAssembly/spec v2.0.0 tree. Files are relative to CoreDir
// and omit the .wast suffix.
type Release2Suite struct {
	CoreDir string
	Files   []string
}

// DiscoverRelease2 validates and enumerates the official Release 2 core-suite
// layout. checkout is the repository root, not test/core and not the preserved
// WebAssembly/testsuite checkout used by the 1.0 baseline.
func DiscoverRelease2(checkout string) (Release2Suite, error) {
	coreDir := filepath.Join(checkout, "test", "core")
	info, err := os.Stat(coreDir)
	if err != nil {
		return Release2Suite{}, fmt.Errorf("WebAssembly 2.0 corpus %q is missing test/core: %w", checkout, err)
	}
	if !info.IsDir() {
		return Release2Suite{}, fmt.Errorf("WebAssembly 2.0 corpus %q has non-directory test/core", checkout)
	}

	var files []string
	err = filepath.WalkDir(coreDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".wast") {
			return nil
		}
		rel, err := filepath.Rel(coreDir, path)
		if err != nil {
			return err
		}
		files = append(files, strings.TrimSuffix(rel, ".wast"))
		return nil
	})
	if err != nil {
		return Release2Suite{}, fmt.Errorf("scan WebAssembly 2.0 core corpus %q: %w", coreDir, err)
	}
	if len(files) == 0 {
		return Release2Suite{}, fmt.Errorf("WebAssembly 2.0 corpus %q has no .wast files under test/core", checkout)
	}
	sort.Strings(files)
	if !contains(files, "i32") || !contains(files, "ref_null") || !contains(files, filepath.Join("simd", "simd_const")) {
		return Release2Suite{}, fmt.Errorf("WebAssembly 2.0 corpus %q does not match the official test/core layout", checkout)
	}
	return Release2Suite{CoreDir: coreDir, Files: files}, nil
}

// Release2Digest hashes every discovered relative path and WAST byte sequence,
// making fixture drift visible even when the file count is unchanged.
func Release2Digest(suite Release2Suite) (string, error) {
	h := sha256.New()
	var lenBuf [8]byte
	for _, rel := range suite.Files {
		pathBytes := []byte(filepath.ToSlash(rel))
		binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(pathBytes)))
		_, _ = h.Write(lenBuf[:])
		_, _ = h.Write(pathBytes)
		data, err := os.ReadFile(filepath.Join(suite.CoreDir, rel+".wast"))
		if err != nil {
			return "", fmt.Errorf("read WebAssembly 2.0 fixture %q: %w", rel, err)
		}
		binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(data)))
		_, _ = h.Write(lenBuf[:])
		_, _ = h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func contains(values []string, want string) bool {
	i := sort.SearchStrings(values, want)
	return i < len(values) && values[i] == want
}
