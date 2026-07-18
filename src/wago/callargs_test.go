//go:build linux && (amd64 || riscv64) && !tinygo

// This file builds fixtures by shelling out to wat2wasm via os/exec, which
// TinyGo does not support; combined with TinyGo's testing package not honoring
// t.Skip/t.Fatal (no runtime.Goexit), it cannot run under TinyGo. Excluded from
// the TinyGo build — the public API is still covered there by the embedded-
// fixture tests in wago_test.go. See docs/tinygo.md.

package wago

import (
	"testing"
)

// TestCallMixedArgKinds guards the direct-arg-write call path: a call whose
// arguments mix constants, locals, and a value computed into a scratch register
// (which can be RAX). The arg writer must use RSI, never RAX, or a const/local
// arg would clobber a register arg before it is stored to the call buffer.
func TestCallMixedArgKinds(t *testing.T) {
	wasm := watToWasmCA(t, `(module
		(func $combine (param i32 i32 i32 i32) (result i32)
			(i32.add
				(i32.add (i32.mul (local.get 0) (i32.const 1000)) (i32.mul (local.get 1) (i32.const 100)))
				(i32.add (i32.mul (local.get 2) (i32.const 10)) (local.get 3))))
		(func (export "f") (param i32) (result i32)
			(call $combine
				(i32.const 1)
				(local.get 0)
				(i32.add (local.get 0) (i32.const 1))
				(i32.const 4))))`)
	c, err := Compile(nil, wasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	// f(2) = combine(1, 2, 3, 4) = 1000 + 200 + 30 + 4 = 1234.
	if r, _ := in.Invoke("f", I32(2)); AsI32(r[0]) != 1234 {
		t.Fatalf("f(2) = %d, want 1234", AsI32(r[0]))
	}
	// f(5) = combine(1, 5, 6, 4) = 1000 + 500 + 60 + 4 = 1564.
	if r, _ := in.Invoke("f", I32(5)); AsI32(r[0]) != 1564 {
		t.Fatalf("f(5) = %d, want 1564", AsI32(r[0]))
	}
}
