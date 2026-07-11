//go:build ((linux && amd64) || arm64) && !tinygo

package wago

import "testing"

// Shared one-shot execution helpers. These are pure public-API (Compile /
// Instantiate / Invoke), so they run on every backend the engine supports; they
// live here (rather than in the historically linux&&amd64 wago_test.go) so
// arch-neutral test files can be widened to arm64 without pulling the whole suite.

// runv compiles, instantiates with no imports, and invokes an export.
func runv(t *testing.T, wasm []byte, export string, args ...uint64) []uint64 {
	t.Helper()
	return runImports(t, wasm, Imports{}, export, args...)
}

// run1 invokes an export taking i32 args and returning one i32.
func run1(t *testing.T, wasm []byte, export string, args ...int32) int32 {
	t.Helper()
	vals := make([]uint64, len(args))
	for i, a := range args {
		vals[i] = I32(a)
	}
	res := runv(t, wasm, export, vals...)
	if len(res) != 1 {
		t.Fatalf("%s: expected 1 result, got %v", export, res)
	}
	return AsI32(res[0])
}

// runImports compiles, instantiates with imports, and invokes an export — the
// pipeline for one-shot runs that need host functions or imported globals.
func runImports(t *testing.T, wasm []byte, imports Imports, export string, args ...uint64) []uint64 {
	t.Helper()
	c, err := Compile(nil, wasm)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke(export, args...)
	if err != nil {
		t.Fatalf("%s: %v", export, err)
	}
	return res
}
