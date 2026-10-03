package hostthunk_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestArm64SyncThunkTransferEncodingsAreChecked(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../../.."))
	for _, test := range []struct {
		path     string
		legacyFn string
		helperFn string
	}{
		{
			filepath.Join(repo, "src/core/runtime/hostthunk/arm64.go"),
			"indirectSync",
			"emitSyncTransfers",
		},
		{
			filepath.Join(repo, "src/core/compiler/backend/railshot/arm64/call.go"),
			"hostIndirectSyncThunk",
			"emitHostThunkTransfers",
		},
	} {
		t.Run(test.legacyFn, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, test.path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			funcs := make(map[string]*ast.FuncDecl)
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					funcs[fn.Name.Name] = fn
				}
			}

			// Prefer the transfer helper when present, while retaining coverage of
			// the original inline implementation in the regression-test commit.
			target := funcs[test.helperFn]
			if target == nil {
				target = funcs[test.legacyFn]
			}
			if target == nil {
				t.Fatalf("neither %s nor %s found in %s", test.helperFn, test.legacyFn, test.path)
			}

			seen := map[string]int{"Load64": 0, "Store64": 0}
			ast.Inspect(target.Body, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.CallExpr:
					if name, ok := arm64TransferCallName(node); ok {
						seen[name]++
					}
				case *ast.ExprStmt:
					if name, ok := arm64TransferExprName(node.X); ok {
						t.Errorf("%s ignores the offset-limited %s result at %s", target.Name.Name, name, fset.Position(node.Pos()))
					}
				case *ast.AssignStmt:
					if len(node.Lhs) == len(node.Rhs) {
						for i, rhs := range node.Rhs {
							ident, blank := node.Lhs[i].(*ast.Ident)
							name, transfer := arm64TransferExprName(rhs)
							if blank && ident.Name == "_" && transfer {
								t.Errorf("%s discards the offset-limited %s result at %s", target.Name.Name, name, fset.Position(node.Pos()))
							}
						}
					}
				}
				return true
			})
			for _, name := range []string{"Load64", "Store64"} {
				if seen[name] == 0 {
					t.Errorf("%s contains no %s transfer; guard would be vacuous", target.Name.Name, name)
				}
			}
		})
	}
}

func arm64TransferExprName(expr ast.Expr) (string, bool) {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			break
		}
		expr = paren.X
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	return arm64TransferCallName(call)
}

func arm64TransferCallName(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Load64" && sel.Sel.Name != "Store64") {
		return "", false
	}
	return sel.Sel.Name, true
}
