package manager

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInternalPackagesDoNotDependOnCommands(t *testing.T) {
	rejectImports(t, "internal", func(importPath string) bool {
		return strings.Contains(importPath, "/cli/manager/commands/")
	})
}

func TestManagerDoesNotDependOnRuntimePackages(t *testing.T) {
	rejectImports(t, ".", func(importPath string) bool {
		return strings.Contains(importPath, "/cli/runtime/")
	})
}

func rejectImports(t *testing.T, root string, rejected func(string) bool) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, spec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			if rejected(importPath) {
				t.Errorf("%s has forbidden import %q", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
