package spectest

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Release3Suite identifies the official WebAssembly 3.0 core corpus within a
// checkout of the WebAssembly/spec wg-3.0 tree. Files are relative to CoreDir
// and omit the .wast suffix.
type Release3Suite struct {
	CoreDir string
	Files   []string
}

// DiscoverRelease3 validates and enumerates the official Release 3 core-suite
// layout. checkout is the repository root, not test/core and not the preserved
// WebAssembly/testsuite checkout used by the 1.0 baseline.
func DiscoverRelease3(checkout string) (Release3Suite, error) {
	coreDir := filepath.Join(checkout, "test", "core")
	info, err := os.Stat(coreDir)
	if err != nil {
		return Release3Suite{}, fmt.Errorf("WebAssembly 3.0 corpus %q is missing test/core: %w", checkout, err)
	}
	if !info.IsDir() {
		return Release3Suite{}, fmt.Errorf("WebAssembly 3.0 corpus %q has non-directory test/core", checkout)
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
		return Release3Suite{}, fmt.Errorf("scan WebAssembly 3.0 core corpus %q: %w", coreDir, err)
	}
	if len(files) == 0 {
		return Release3Suite{}, fmt.Errorf("WebAssembly 3.0 corpus %q has no .wast files under test/core", checkout)
	}
	sort.Strings(files)

	// These sentinels distinguish the official Release 3 aggregate from the
	// Release 2 tree and from the legacy proposal snapshots. Keep every mandatory
	// runtime family represented so a partial or wrong checkout fails closed.
	required := []string{
		"i32",
		"const",
		"return_call",
		"call_ref",
		filepath.Join("gc", "struct"),
		filepath.Join("exceptions", "throw"),
		filepath.Join("multi-memory", "memory-multi"),
		filepath.Join("memory64", "memory64"),
		filepath.Join("memory64", "table64"),
		filepath.Join("relaxed-simd", "relaxed_laneselect"),
	}
	for _, name := range required {
		if !contains(files, name) {
			return Release3Suite{}, fmt.Errorf("WebAssembly 3.0 corpus %q does not match the official wg-3.0 test/core layout: missing %s.wast", checkout, name)
		}
	}
	return Release3Suite{CoreDir: coreDir, Files: files}, nil
}
