//go:build ((linux && (amd64 || riscv64)) || arm64) && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// Execution coverage for the immutable local-table call_indirect specialization
// (monomorphic direct call, immutable-local home/tag elision, and uniform-type
// check elision). These run through the real runtime — which populates the funcref
// table — so they validate that the specialized dispatch reaches the right target
// and still traps on an out-of-bounds index. The backend-package tests only assert
// the specialization fires at compile time; call_indirect cannot execute there
// because that harness never installs a table.

// callIndirectModule builds caller(idx,a,b)->i32 that does
// `call_indirect (i32,i32)->i32` through table 0, whose active element lists the
// given local target function indices. targets index into {add(func1), sub(func2)}.
func callIndirectModule(minSize uint32, targets ...uint32) []byte {
	i32 := []wasm.ValType{wasm.I32}
	twoI32 := []wasm.ValType{wasm.I32, wasm.I32}
	elem := []byte{0x00, 0x41, 0x00, 0x0b} // active, table 0, offset i32.const 0
	elem = append(elem, wasmtest.ULEB(uint32(len(targets)))...)
	for _, tgt := range targets {
		elem = append(elem, wasmtest.ULEB(tgt)...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, i32), // type0: caller
			wasmtest.FuncType(twoI32, i32),                                       // type1: add/sub
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(append([]byte{0x70, 0x00}, wasmtest.ULEB(minSize)...))), // funcref table
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("caller", 0, 0))),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(
			// caller: local.get 1(a); local.get 2(b); local.get 0(idx); call_indirect type1 table0
			wasmtest.Code([]byte{0x20, 0x01, 0x20, 0x02, 0x20, 0x00, 0x11, 0x01, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}), // add: a+b
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6b, 0x0b}), // sub: a-b
		)),
	)
}

func TestMonomorphicCallIndirectExec(t *testing.T) {
	// Single-target immutable table → the monomorphic direct-call specialization.
	in, err := Instantiate(MustCompile(callIndirectModule(1, 1)), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	if got, err := in.Invoke("caller", 0, 3, 4); err != nil || len(got) != 1 || got[0] != 7 {
		t.Fatalf("caller(0,3,4) = %v, %v; want 7", got, err)
	}
	// Out-of-bounds index (table length 1) must still trap — the specialization
	// preserves the bounds check.
	if _, err := in.Invoke("caller", 1, 3, 4); err == nil {
		t.Fatal("out-of-bounds call_indirect index did not trap")
	}
}

// TestImmutableTableInitializerActiveOverrideExec guards the fix for the
// monomorphic analysis: a table initializer prefills every slot with a default
// target, so a table whose active element overrides one slot with a DIFFERENT
// function is not monomorphic. It must dispatch per-slot (initializer target for
// un-overridden slots, active target for the overridden one), not collapse to a
// single direct call. The linux-only table_ops suite covers the same shape; this
// duplicate also runs on darwin/arm64.
func TestImmutableTableInitializerActiveOverrideExec(t *testing.T) {
	// type0: (i32)->i32 [callAt]; type1: ()->i32 [ret42, ret7].
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		// table funcref, min 3, initializer = ref.func 1 (ret42).
		wasmtest.Section(4, wasmtest.Vec([]byte{0x40, 0x00, 0x70, 0x00, 0x03, 0xd2, 0x01, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("callAt", 0, 0))),
		// active element: table[1] = func 2 (ret7), overriding the initializer.
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x01, 0x0b, 0x01, 0x02})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x11, 0x01, 0x00, 0x0b}), // callAt: local.get 0; call_indirect type1 table0
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),                   // ret42
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),                   // ret7
		)),
	)
	in, err := Instantiate(MustCompile(mod), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	for idx, want := range map[uint64]uint64{0: 42, 1: 7, 2: 42} {
		if got, err := in.Invoke("callAt", idx); err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("callAt(%d) = %v, %v; want %d", idx, got, err, want)
		}
	}
}

func TestImmutableMultiTargetCallIndirectExec(t *testing.T) {
	// Two same-type targets → immutable-local (home/tag elided) + uniform-type
	// check elision, but a real runtime index selects the target.
	in, err := Instantiate(MustCompile(callIndirectModule(2, 1, 2)), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	if got, err := in.Invoke("caller", 0, 10, 3); err != nil || len(got) != 1 || got[0] != 13 {
		t.Fatalf("caller(0,10,3) via add = %v, %v; want 13", got, err)
	}
	if got, err := in.Invoke("caller", 1, 10, 3); err != nil || len(got) != 1 || got[0] != 7 {
		t.Fatalf("caller(1,10,3) via sub = %v, %v; want 7", got, err)
	}
}
