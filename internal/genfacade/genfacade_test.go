package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestFacadeUpToDate fails if the committed root wago.go does not match what the
// generator produces from src/wago — i.e. someone changed the public API (or
// wago.go) without running `go generate ./...`. It regenerates in memory and
// compares, so it never mutates the working tree.
func TestFacadeUpToDate(t *testing.T) {
	root := moduleRoot(t)
	want, err := generate(root)
	if err != nil {
		t.Fatalf("generate facade: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, outFile))
	if err != nil {
		t.Fatalf("read %s: %v", outFile, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s is out of date with src/wago's exported API; run `go generate ./...` and commit the result", outFile)
	}
}

func TestGeneratorTypeRenderingAndBlocks(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"Thing", "Thing"}, {"*Thing", "*Thing"}, {"[]Thing", "[]Thing"}, {"[4]byte", "[4]byte"}, {"map[string][]byte", "map[string][]byte"}, {"interface{}", "interface{}"},
	} {
		expr, err := parser.ParseExpr(tc.src)
		if err != nil {
			t.Fatal(err)
		}
		got, err := renderType(expr)
		if err != nil || got != tc.want {
			t.Fatalf("renderType(%s) = %q, %v", tc.src, got, err)
		}
	}
	if got, err := renderType(&ast.Ellipsis{Elt: ast.NewIdent("string")}); err != nil || got != "...string" {
		t.Fatalf("renderType(ellipsis) = %q, %v", got, err)
	}
	if got, err := renderType(&ast.InterfaceType{Methods: &ast.FieldList{}}); err != nil || got != "interface{}" {
		t.Fatalf("renderType(empty interface) = %q, %v", got, err)
	}
	if _, err := renderType(&ast.ArrayType{Len: ast.NewIdent("N"), Elt: ast.NewIdent("byte")}); err == nil {
		t.Fatal("non-literal array length accepted")
	}
	for _, src := range []string{"func()", "chan int", "interface{ M() }"} {
		expr, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := renderType(expr); err == nil {
			t.Fatalf("renderType(%s) unexpectedly succeeded", src)
		}
	}
	var b bytes.Buffer
	emitConstLikeBlock(&b, "const", nil)
	if b.Len() != 0 {
		t.Fatalf("empty const-like block = %q", b.String())
	}
	emitConstLikeBlock(&b, "type", []string{"B", "A"})
	if got := b.String(); got != "type (\n\tB = impl.B\n\tA = impl.A\n)\n\n" {
		t.Fatalf("const-like block = %q", got)
	}
	if err := emitFunc(&b, &ast.FuncDecl{Name: ast.NewIdent("F"), Type: &ast.FuncType{Params: &ast.FieldList{List: []*ast.Field{{Type: ast.NewIdent("int")}}}, Results: &ast.FieldList{List: []*ast.Field{{Type: ast.NewIdent("error")}}}}}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b.Bytes(), []byte("func F(p0 int) error { return impl.F(p0) }")) {
		t.Fatalf("emitted function = %q", b.String())
	}
}

// moduleRoot returns the repository root relative to this test file, which lives
// at <root>/internal/genfacade.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file location")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}
