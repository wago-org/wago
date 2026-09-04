package railshot

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

// TestProductionPolicyRejectsWorkloadIdentity keeps benchmark corpora as
// qualification inputs, never compilation inputs. Production lowering may
// inspect validated instructions, effects, resource estimates, and target
// features; names, producer fingerprints, hashes, and memorized body prefixes
// are not valid optimization selectors.
func TestProductionPolicyRejectsWorkloadIdentity(t *testing.T) {
	workloadMarkers := []string{
		"bench/corpus", "many_funcs", "json-as", "utf-as", "blake-as",
		"xjb-mulhi", "swar-pack", "regexmatch", "esbuild", "sqlite", "ruby",
	}
	forbiddenImports := map[string]bool{
		"encoding/hex": true,
		"hash":         true,
	}
	forbiddenByteMatchers := map[string]bool{
		"HasPrefix": true,
		"HasSuffix": true,
		"Contains":  true,
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		bytesNames := map[string]bool{}
		for _, imp := range file.Imports {
			name, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if forbiddenImports[name] || strings.HasPrefix(name, "crypto/") || strings.HasPrefix(name, "hash/") {
				t.Errorf("%s imports %q; production Railshot policy must not fingerprint workloads", path, name)
			}
			if name == "bytes" {
				local := "bytes"
				if imp.Name != nil {
					local = imp.Name.Name
				}
				if local == "." {
					t.Errorf("%s dot-imports bytes; the workload-identity audit requires a named import", path)
				} else {
					bytesNames[local] = true
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.BasicLit:
				if node.Kind != token.STRING {
					break
				}
				value, err := strconv.Unquote(node.Value)
				if err != nil {
					break
				}
				value = strings.ToLower(value)
				for _, marker := range workloadMarkers {
					if strings.Contains(value, marker) {
						t.Errorf("%s:%d contains production workload marker %q", path, fset.Position(node.Pos()).Line, marker)
					}
				}
			case *ast.SelectorExpr:
				if path != filepath.Join("amd64", "stats.go") && path != filepath.Join("arm64", "stats.go") &&
					(node.Sel.Name == "NameSec" || node.Sel.Name == "RawNameSecPayload" || node.Sel.Name == "ModuleName" || node.Sel.Name == "FunctionNames") {
					t.Errorf("%s:%d reads %s outside diagnostic naming", path, fset.Position(node.Pos()).Line, node.Sel.Name)
				}
			case *ast.CallExpr:
				selector, ok := node.Fun.(*ast.SelectorExpr)
				if !ok || !forbiddenByteMatchers[selector.Sel.Name] {
					break
				}
				pkg, ok := selector.X.(*ast.Ident)
				if ok && bytesNames[pkg.Name] {
					t.Errorf("%s:%d calls bytes.%s; decode instructions instead of matching body fingerprints", path, fset.Position(node.Pos()).Line, selector.Sel.Name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
