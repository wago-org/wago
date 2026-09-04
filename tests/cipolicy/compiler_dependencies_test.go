package cipolicy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCompilerEngineDependencyBoundaries(t *testing.T) {
	tests := []struct {
		root      string
		forbidden string
	}{
		{"../../src/core/compiler/backend/dragline", "/compiler/backend/railshot"},
		{"../../src/core/compiler/backend/railshot", "/compiler/backend/dragline"},
		{"../../src/core/runtime", "/compiler/backend/dragline"},
		{"../../src/core/runtime", "/compiler/backend/railshot"},
	}
	for _, test := range tests {
		t.Run(filepath.Base(test.root)+test.forbidden, func(t *testing.T) {
			err := filepath.WalkDir(filepath.Clean(test.root), func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
					return nil
				}
				file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, spec := range file.Decls {
					decl, ok := spec.(*ast.GenDecl)
					if !ok || decl.Tok != token.IMPORT {
						continue
					}
					for _, item := range decl.Specs {
						pathSpec := item.(*ast.ImportSpec)
						importPath, err := strconv.Unquote(pathSpec.Path.Value)
						if err != nil {
							return err
						}
						if strings.Contains(importPath, test.forbidden) {
							t.Errorf("%s imports forbidden sibling internals %q", path, importPath)
						}
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
