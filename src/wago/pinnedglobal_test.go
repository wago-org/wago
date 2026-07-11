//go:build linux && amd64 && !tinygo

// Like callargs_test.go, this builds fixtures via wat2wasm through os/exec, which
// TinyGo does not support, so it is excluded from the TinyGo build. See
// docs/tinygo.md.

package wago

import (
	"testing"
)

// TestPinnedGlobalAccumulateAndPersist exercises a mutable i64 global pinned in
// a register: the get/add/set loop, the trailing read, write-back to the cell at
// return, and reload at the next invocation's prologue (cross-invoke state).
func TestPinnedGlobalAccumulateAndPersist(t *testing.T) {
	wasm := watToWasm(t, `(module
		(global $g (mut i64) (i64.const 0))
		(func (export "accumulate") (param i32) (result i64)
			(block $brk (loop $lp
				(br_if $brk (i32.eqz (local.get 0)))
				(global.set $g (i64.add (global.get $g) (i64.extend_i32_u (local.get 0))))
				(local.set 0 (i32.sub (local.get 0) (i32.const 1)))
				(br $lp)))
			(global.get $g))
		(func (export "get") (result i64) (global.get $g)))`)
	c, err := Compile(nil, wasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	if r, _ := in.Invoke("accumulate", I32(5)); AsI64(r[0]) != 15 {
		t.Fatalf("accumulate(5) = %d, want 15", AsI64(r[0]))
	}
	// Cross-invoke persistence: the cell must hold 15 (written back at return,
	// reloaded into the register at the next prologue).
	if r, _ := in.Invoke("get"); AsI64(r[0]) != 15 {
		t.Fatalf("get after first = %d, want 15", AsI64(r[0]))
	}
	if r, _ := in.Invoke("accumulate", I32(3)); AsI64(r[0]) != 21 {
		t.Fatalf("accumulate(3) = %d, want 21 (15+6)", AsI64(r[0]))
	}
	if r, _ := in.Invoke("get"); AsI64(r[0]) != 21 {
		t.Fatalf("get after second = %d, want 21", AsI64(r[0]))
	}
}

// TestPinnedGlobalAcrossCall is the critical correctness test for the pinned-
// global call boundary: the caller writes $g=5 (register), calls a function that
// adds 10 to the same global, then reads it. Requires write-back-before-call,
// the callee's own prologue reload + epilogue write-back, and reload-after-call.
func TestPinnedGlobalAcrossCall(t *testing.T) {
	wasm := watToWasm(t, `(module
		(global $g (mut i64) (i64.const 0))
		(func $bump (global.set $g (i64.add (global.get $g) (i64.const 10))))
		(func (export "f") (result i64)
			(global.set $g (i64.const 5))
			(call $bump)
			(global.get $g)))`)
	c, err := Compile(nil, wasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if r, _ := in.Invoke("f"); AsI64(r[0]) != 15 {
		t.Fatalf("f() = %d, want 15 (5 + bump 10)", AsI64(r[0]))
	}
}

// TestPinnedGlobalAliasCapture guards materializeGlobalRefs: a pending pinned
// read of $g keeps the OLD value when global.set overwrites the register.
func TestPinnedGlobalAliasCapture(t *testing.T) {
	wasm := watToWasm(t, `(module
		(global $g (mut i64) (i64.const 7))
		(func (export "f") (result i64)
			global.get $g
			i64.const 100 global.set $g
			global.get $g i64.add))`)
	c, err := Compile(nil, wasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if r, _ := in.Invoke("f"); AsI64(r[0]) != 107 {
		t.Fatalf("f() = %d, want 107 (old 7 + new 100)", AsI64(r[0]))
	}
}
