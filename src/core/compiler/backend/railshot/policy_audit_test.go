package railshot

import (
	"fmt"
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
		problems, err := productionPolicyProblems(path, fset, file)
		if err != nil {
			return err
		}
		for _, problem := range problems {
			t.Error(problem)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func productionPolicyProblems(path string, fset *token.FileSet, file *ast.File) ([]string, error) {
	var problems []string
	report := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	workloadMarkers := []string{
		"corpus/workloads", "many_funcs", "json-as", "utf-as", "blake-as",
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
	bytesNames := map[string]bool{}
	for _, imp := range file.Imports {
		name, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		if (forbiddenImports[name] || strings.HasPrefix(name, "crypto/") || strings.HasPrefix(name, "hash/")) && !(path == filepath.Join("amd64", "gc_lifetime.go") && name == "crypto/sha256") {
			report("%s imports %q; production Railshot policy must not fingerprint workloads", path, name)
		}
		if name == "bytes" {
			local := "bytes"
			if imp.Name != nil {
				local = imp.Name.Name
			}
			if local == "." {
				report("%s dot-imports bytes; the workload-identity audit requires a named import", path)
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
					report("%s:%d contains production workload marker %q", path, fset.Position(node.Pos()).Line, marker)
				}
			}
		case *ast.SelectorExpr:
			if path != filepath.Join("amd64", "stats.go") && path != filepath.Join("arm64", "stats.go") && !productionMetadataBudgetRead(path, file, node) &&
				(node.Sel.Name == "NameSec" || node.Sel.Name == "RawNameSecPayload" || node.Sel.Name == "ModuleName" || node.Sel.Name == "FunctionNames") {
				report("%s:%d reads %s outside diagnostic naming or bounded metadata accounting", path, fset.Position(node.Pos()).Line, node.Sel.Name)
			}
		case *ast.CallExpr:
			selector, ok := node.Fun.(*ast.SelectorExpr)
			if !ok || !forbiddenByteMatchers[selector.Sel.Name] {
				break
			}
			pkg, ok := selector.X.(*ast.Ident)
			if ok && bytesNames[pkg.Name] && !lifetimeDiagnosticNegativeGate(path, file, node) {
				report("%s:%d calls bytes.%s; decode instructions instead of matching body fingerprints", path, fset.Position(node.Pos()).Line, selector.Sel.Name)
			}
		}
		return true
	})
	return problems, nil
}

// Only this resource-accounting body may read name metadata outside the existing
// diagnostic files. Imports, workload markers and byte matchers remain audited.
func productionMetadataBudgetRead(path string, file *ast.File, node *ast.SelectorExpr) bool {
	if path != filepath.Join("riscv64", "decoded_metadata_budget.go") {
		return false
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == "admitSWARDecodedMetadataBudget" && fn.Body != nil && node.Pos() >= fn.Body.Pos() && node.End() <= fn.Body.End() {
			return true
		}
	}
	return false
}

func TestProductionPolicyMetadataBudgetScope(t *testing.T) {
	helper := "func admitSWARDecodedMetadataBudget() { _ = m.NameSec; _ = m.RawNameSecPayload; _ = m.NameSec.ModuleName; _ = m.NameSec.FunctionNames }"
	exactPath := filepath.Join("riscv64", "decoded_metadata_budget.go")
	for _, tc := range []struct{ name, path, source, reject string }{
		{"exact-resource-helper", exactPath, helper, ""},
		{"neighbor-function", exactPath, "func optimizer() { _ = m.NameSec }", "reads NameSec"},
		{"other-file", filepath.Join("riscv64", "swar_module.go"), helper, "reads NameSec"},
		{"global-initializer", exactPath, "var names = m.NameSec; " + helper, "reads NameSec"},
		{"method", exactPath, "func (x T) admitSWARDecodedMetadataBudget() { _ = m.NameSec }", "reads NameSec"},
		{"fingerprint-import", exactPath, "import \"hash\"; " + helper, "imports"},
		{"fingerprint-matcher", exactPath, "import b \"bytes\"; func admitSWARDecodedMetadataBudget() { _ = m.NameSec; b.HasPrefix(a, a) }", "calls bytes.HasPrefix"},
		{"dot-import", exactPath, "import . \"bytes\"; " + helper, "dot-imports bytes"},
		{"workload-marker", exactPath, "func admitSWARDecodedMetadataBudget() { _ = m.NameSec; _ = \"sqlite\" }", "workload marker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tc.path, "package riscv64; "+tc.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			problems, err := productionPolicyProblems(tc.path, fset, file)
			if err != nil {
				t.Fatal(err)
			}
			if tc.reject == "" {
				if len(problems) != 0 {
					t.Fatal(problems)
				}
				return
			}
			found := false
			for _, problem := range problems {
				if strings.Contains(problem, tc.reject) {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %q rejection: %v", tc.reject, problems)
			}
		})
	}
}

// Analyze reports never select lowering: Off/Analyze code and root-map identity
// is checked in TestGCLifetimeCompileIdentity. The hash in this exact diagnostic
// file labels a report only. Its negative gate permits one necessary condition,
// absence of the GC opcode prefix, not matching a workload or body prefix.
func lifetimeDiagnosticNegativeGate(path string, file *ast.File, call *ast.CallExpr) bool {
	if path != filepath.Join("amd64", "gc_lifetime.go") || len(call.Args) != 2 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Contains" {
		return false
	}
	body, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok || body.Sel.Name != "BodyBytes" {
		return false
	}
	c, ok := body.X.(*ast.Ident)
	if !ok || c.Name != "c" {
		return false
	}
	needle, ok := call.Args[1].(*ast.CompositeLit)
	if !ok || len(needle.Elts) != 1 {
		return false
	}
	typ, ok := needle.Type.(*ast.ArrayType)
	if !ok || typ.Len != nil {
		return false
	}
	elem, ok := typ.Elt.(*ast.Ident)
	if !ok || elem.Name != "byte" {
		return false
	}
	value, ok := needle.Elts[0].(*ast.BasicLit)
	if !ok || value.Kind != token.INT {
		return false
	}
	n, err := strconv.ParseUint(value.Value, 0, 8)
	if err != nil || n != 0xfb {
		return false
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == "analyzeGCLifetime" && fn.Body != nil && call.Pos() >= fn.Body.Pos() && call.End() <= fn.Body.End() {
			return true
		}
	}
	return false
}

func TestLifetimeDiagnosticPolicyScope(t *testing.T) {
	exact := filepath.Join("amd64", "gc_lifetime.go")
	for _, tc := range []struct {
		name, path, src string
		reject          bool
	}{
		{"diagnostic", exact, `import "crypto/sha256"; import "bytes"; func analyzeGCLifetime() { bytes.Contains(c.BodyBytes, []byte{0xfb}) }`, false},
		{"other-file", filepath.Join("amd64", "gc_struct.go"), `import "crypto/sha256"`, true},
		{"other-hash", exact, `import "crypto/sha512"`, true},
		{"other-function", exact, `import "bytes"; func lower() { bytes.Contains(c.BodyBytes, []byte{0xfb}) }`, true},
		{"other-needle", exact, `import "bytes"; func analyzeGCLifetime() { bytes.Contains(c.BodyBytes, []byte{0x41}) }`, true},
		{"body-prefix", exact, `import "bytes"; func analyzeGCLifetime() { bytes.HasPrefix(c.BodyBytes, []byte{0xfb}) }`, true},
		{"multiple-bytes", exact, `import "bytes"; func analyzeGCLifetime() { bytes.Contains(c.BodyBytes, []byte{0xfb, 0}) }`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := token.NewFileSet()
			f, err := parser.ParseFile(fs, tc.path, "package amd64; "+tc.src, 0)
			if err != nil {
				t.Fatal(err)
			}
			problems, err := productionPolicyProblems(tc.path, fs, f)
			if err != nil {
				t.Fatal(err)
			}
			if (len(problems) > 0) != tc.reject {
				t.Fatalf("reject=%v: %v", tc.reject, problems)
			}
		})
	}
}
