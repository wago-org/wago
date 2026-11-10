package arm64

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Run on every host: native execution is not needed to enforce error handling
// at every encoder branch patch call, including rarely selected lowering paths.
func TestBranchPatchResultsAreChecked(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			stmt, ok := node.(*ast.ExprStmt)
			if !ok {
				return true
			}
			call, ok := stmt.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if ok && (sel.Sel.Name == "PatchBranch19" || sel.Sel.Name == "PatchBranch26") {
				t.Errorf("%s: encoder branch patch result is ignored", fset.Position(call.Pos()))
			}
			return true
		})
	}
}
