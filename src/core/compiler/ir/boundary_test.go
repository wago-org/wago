package ir

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const irImportPath = "github.com/wago-org/wago/src/core/compiler/ir"

// TestIRRemainsOffProductionPath prevents the research/debug SSA package from
// becoming an accidental production execution tier.
func TestIRRemainsOffProductionPath(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	irDir := filepath.Dir(file)
	root := filepath.Clean(filepath.Join(irDir, "..", "..", "..", ".."))

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".tmp", "warp", "wasm3":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || path == file || strings.HasPrefix(path, irDir+string(filepath.Separator)) {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if importPath == irImportPath {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("production source %s imports off-path compiler IR", filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
