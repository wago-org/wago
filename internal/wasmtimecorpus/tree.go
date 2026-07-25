package wasmtimecorpus

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ValidateCorpusTree checks the complete prospective fixture tree, including
// exact per-mode artifact shapes and rejection of orphan files/directories.
func ValidateCorpusTree(root string, fixtures []Fixture) error {
	expectedFiles := map[string]bool{}
	expectedDirs := map[string]bool{".": true}
	for _, fixture := range fixtures {
		dirRel := strings.TrimSuffix(fixture.Path, ".wast")
		dir := filepath.Join(root, filepath.FromSlash(dirRel))
		for current := dirRel; current != "." && current != ""; current = filepath.ToSlash(filepath.Dir(filepath.FromSlash(current))) {
			expectedDirs[current] = true
		}
		sourceRel := pathJoinSlash(dirRel, "source.wast")
		if err := requireRegularFile(filepath.Join(dir, "source.wast")); err != nil {
			return fmt.Errorf("%s: %w", fixture.Path, err)
		}
		expectedFiles[sourceRel] = true
		switch fixture.Mode {
		case ModeWASTJSON:
			if _, err := ValidateWASTJSONFixture(dir); err != nil {
				return fmt.Errorf("%s: %w", fixture.Path, err)
			}
			expectedFiles[pathJoinSlash(dirRel, "commands.json")] = true
			matches, err := filepath.Glob(filepath.Join(dir, "commands.*.wasm"))
			if err != nil {
				return err
			}
			for _, match := range matches {
				expectedFiles[pathJoinSlash(dirRel, filepath.Base(match))] = true
			}
		case ModeDirectGo, ModeDirectInvalid, ModeDirectConcurrency:
			matches, err := filepath.Glob(filepath.Join(dir, "module.*.wasm"))
			if err != nil {
				return err
			}
			if len(matches) == 0 {
				return fmt.Errorf("%s has no module.*.wasm artifacts", fixture.Path)
			}
			sort.Strings(matches)
			for i, match := range matches {
				name := filepath.Base(match)
				want := "module." + strconv.Itoa(i) + ".wasm"
				if name != want {
					return fmt.Errorf("%s direct artifacts are not contiguous: got %q, want %q", fixture.Path, name, want)
				}
				if err := requireRegularFile(match); err != nil {
					return fmt.Errorf("%s: %w", fixture.Path, err)
				}
				expectedFiles[pathJoinSlash(dirRel, name)] = true
			}
		default:
			return fmt.Errorf("%s has unknown mode %q", fixture.Path, fixture.Mode)
		}
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("corpus tree contains symlink %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if !expectedDirs[rel] {
				return fmt.Errorf("corpus tree contains orphan directory %q", rel)
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("corpus tree contains non-regular file %q", rel)
		}
		if !expectedFiles[rel] {
			return fmt.Errorf("corpus tree contains orphan file %q", rel)
		}
		return nil
	})
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

func pathJoinSlash(parts ...string) string {
	return filepath.ToSlash(filepath.Join(parts...))
}
