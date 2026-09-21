//go:build arm64 && !tinygo && (linux || darwin)

package wago

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestPreparedIntCallBlockDefaultARM64(t *testing.T) {
	if !preparedIntCallBlockDefault {
		t.Fatal("ARM64 call block defaults off; the prepared struct thunk is faster")
	}
}

func TestPreparedDirectARM64IgnoresUnusedModuleMemory(t *testing.T) {
	beforeCallBlock := preparedIntCallBlockEnabled
	preparedIntCallBlockEnabled = true
	defer func() { preparedIntCallBlockEnabled = beforeCallBlock }()

	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x00})), // one zero-page memory
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("add", 0, 0),
			wasmtest.ExportEntry("size", 0, 1),
			wasmtest.ExportEntry("call_size", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("memory-independent function did not select the ARM64 direct prepared entry")
	}
	if !compiled.directPreparedLightAt(0) {
		t.Fatal("caller-clobber-only function did not select the light ARM64 entry thunk")
	}
	if !compiled.directPreparedBoundedAt(0) {
		t.Fatal("straight-line caller-clobber-only function did not select the bounded ARM64 entry thunk")
	}
	if compiled.directPreparedAt(1) {
		t.Fatal("memory.size function selected the ARM64 direct prepared entry")
	}
	if compiled.directPreparedAt(2) {
		t.Fatal("function calling memory.size selected the ARM64 direct prepared entry")
	}
	directRollback, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("prepared-direct-entry", false), module)
	if err != nil {
		t.Fatalf("compile direct rollback: %v", err)
	}
	if directRollback.directPreparedAt(0) || directRollback.directPreparedLightAt(0) || directRollback.directPreparedBoundedAt(0) {
		t.Fatalf("direct rollback retained direct/light/bounded metadata = %v/%v/%v", directRollback.directPreparedAt(0), directRollback.directPreparedLightAt(0), directRollback.directPreparedBoundedAt(0))
	}
	rollbackInstance, err := Instantiate(directRollback, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate direct rollback: %v", err)
	}
	defer rollbackInstance.Close()
	rollbackFunction, err := rollbackInstance.WasmFunc("add")
	if err != nil {
		t.Fatalf("prepare direct rollback: %v", err)
	}
	if rollbackFunction.directIntFast || rollbackFunction.directIntLight || rollbackFunction.directIntBounded {
		t.Fatalf("direct rollback prepared direct/light/bounded path = %v/%v/%v", rollbackFunction.directIntFast, rollbackFunction.directIntLight, rollbackFunction.directIntBounded)
	}
	if got, err := rollbackFunction.Invoke(20, 22); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("direct rollback add(20,22) = %v, %v; want 42", got, err)
	}
	rollback, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("prepared-light-entry", false), module)
	if err != nil {
		t.Fatalf("compile rollback: %v", err)
	}
	if !rollback.directPreparedAt(0) || rollback.directPreparedLightAt(0) || !rollback.directPreparedBoundedAt(0) {
		t.Fatalf("light rollback direct/light/bounded selection = %v/%v/%v, want true/false/true", rollback.directPreparedAt(0), rollback.directPreparedLightAt(0), rollback.directPreparedBoundedAt(0))
	}
	boundedRollback, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("prepared-bounded-entry", false), module)
	if err != nil {
		t.Fatalf("compile bounded rollback: %v", err)
	}
	if !boundedRollback.directPreparedAt(0) || !boundedRollback.directPreparedLightAt(0) || boundedRollback.directPreparedBoundedAt(0) {
		t.Fatalf("bounded rollback direct/light/bounded selection = %v/%v/%v, want true/true/false", boundedRollback.directPreparedAt(0), boundedRollback.directPreparedLightAt(0), boundedRollback.directPreparedBoundedAt(0))
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("add")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directIntFast {
		t.Fatal("memory-independent function did not prepare the ARM64 direct integer entry")
	}
	if !fn.directIntLight {
		t.Fatal("memory-independent function did not retain the light entry proof")
	}
	if !fn.directIntBounded {
		t.Fatal("memory-independent function did not retain the bounded entry proof")
	}
	if fn.directIntMode != preparedIntCallBlock {
		t.Fatal("bounded light function did not select the immutable call block")
	}
	got, err := fn.Invoke(20, 22)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("add(20,22) = %v, %v; want 42", got, err)
	}
	size, err := in.WasmFunc("size")
	if err != nil {
		t.Fatalf("prepare size: %v", err)
	}
	if size.directIntFast {
		t.Fatal("memory size prepared the ARM64 direct integer entry")
	}
	if got, err := size.Invoke(); err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("WasmFunc memory.size() = %v, %v; want 0", got, err)
	}

	preparedIntCallBlockEnabled = false
	fallback, err := in.WasmFunc("add")
	if err != nil {
		t.Fatalf("prepare call-block rollback: %v", err)
	}
	if fallback.directIntMode == preparedIntCallBlock {
		t.Fatal("call-block rollback retained the call block")
	}
	if got, err := fallback.Invoke(20, 22); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("call-block rollback add(20,22) = %v, %v; want 42", got, err)
	}
}

func TestDraglineARM64MemoryGrowPreservesLiveRegisters(t *testing.T) {
	module := watToWasmCA(t, `(module
		(memory 1 4)
		(func (export "run")
			(param i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32 i32)
			(result i32)
			(local.get 0)
			(memory.grow)
			(drop)
			(local.get 0)
			(local.get 1) (i32.add)
			(local.get 2) (i32.add)
			(local.get 3) (i32.add)
			(local.get 4) (i32.add)
			(local.get 5) (i32.add)
			(local.get 6) (i32.add)
			(local.get 7) (i32.add)
			(local.get 8) (i32.add)
			(local.get 9) (i32.add)
			(local.get 10) (i32.add)
			(local.get 11) (i32.add)))`)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run", 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12)
	if err != nil || len(got) != 1 || got[0] != 78 {
		t.Fatalf("run = %v, %v; want 78", got, err)
	}
}

func TestPreparedDirectARM64I64HashLoop(t *testing.T) {
	body := []byte{
		0x42, 0x00, 0x21, 0x01, 0x02, 0x40, 0x03, 0x40, 0x20, 0x00, 0x45, 0x0d, 0x01,
		0x20, 0x01, 0x20, 0x00, 0xac, 0x42, 0xb1, 0xf3, 0xdd, 0xf1, 0x09, 0x7e, 0x7c, 0x21, 0x01,
		0x20, 0x01, 0x20, 0x01, 0x42, 0x0d, 0x88, 0x85, 0x21, 0x01,
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, 0x0c, 0x00, 0x0b, 0x0b, 0x20, 0x01, 0x0b,
	}
	function := append([]byte{0x01, 0x01, 0x7e}, body...)
	code := append(wasmtest.ULEB(uint32(len(function))), function...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	fn, err := instance.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if !fn.directIntFast || fn.directLeafIntFast {
		t.Fatalf("hash loop selected direct=%t leaf=%t, want interruptible direct entry", fn.directIntFast, fn.directLeafIntFast)
	}
	for _, count := range []uint32{0, 1, 2, 3, 10, 101} {
		var want uint64
		for n := count; n != 0; n-- {
			want += uint64(int64(int32(n))) * uint64(0x9e3779b1)
			want ^= want >> 13
		}
		got, err := fn.Invoke(uint64(count))
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("run(%d) = %v, %v; want %#x", count, got, err, want)
		}
	}
}

func TestPreparedDirectARM64AdditivePairLoop(t *testing.T) {
	body := []byte{
		0x42, 0x00, 0x21, 0x01, // a = 0
		0x42, 0x01, 0x21, 0x02, // b = 1
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, 0x45, 0x0d, 0x01, // break when n == 0
		0x20, 0x01, 0x20, 0x02, 0x7c, 0x21, 0x03, // next = a + b
		0x20, 0x02, 0x21, 0x01, // a = b
		0x20, 0x03, 0x21, 0x02, // b = next
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // n--
		0x0c, 0x00, 0x0b, 0x0b, // continue; end loop/block
		0x20, 0x01, 0x0b,
	}
	function := append([]byte{0x01, 0x03, 0x7e}, body...)
	code := append(wasmtest.ULEB(uint32(len(function))), function...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	fn, err := instance.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	fibonacci := func(n uint32) uint64 {
		a, b := uint64(0), uint64(1)
		for bit := uint32(1 << 31); bit != 0; bit >>= 1 {
			d := a * (2*b - a)
			e := a*a + b*b
			if n&bit == 0 {
				a, b = d, e
			} else {
				a, b = e, d+e
			}
		}
		return a
	}
	for _, count := range []uint32{0, 1, 2, 3, 10, 63, 64, 101, 15_000_000, 1 << 31, ^uint32(0)} {
		got, err := fn.Invoke(uint64(count))
		want := fibonacci(count)
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("run(%d) = %v, %v; want %#x", count, got, err, want)
		}
	}
}

func TestPreparedDirectARM64CallIndirectAndTrapRecovery(t *testing.T) {
	twoI32 := []wasm.ValType{wasm.I32, wasm.I32}
	threeI32 := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
	elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x04, 0x00, 0x01, 0x02, 0x03}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(twoI32, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(threeI32, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x04})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("caller", 0, 4))),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6b, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6c, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x73, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x01, 0x20, 0x02, 0x20, 0x00, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
	for _, tc := range []struct {
		name string
		mode BoundsCheckMode
	}{{"explicit", BoundsChecksExplicit}, {"signals", BoundsChecksSignalsBased}} {
		if tc.mode == BoundsChecksSignalsBased && !GuardPageSupported() {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			testPreparedDirectARM64CallIndirectAndTrapRecovery(t, module, tc.mode)
		})
	}
}

func testPreparedDirectARM64CallIndirectAndTrapRecovery(t *testing.T, module []byte, mode BoundsCheckMode) {
	t.Helper()
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithBoundsChecks(mode), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("call_indirect caller did not select the ARM64 direct prepared entry")
	}
	if !compiled.directPreparedLightAt(0) || !compiled.directPreparedBoundedAt(0) {
		t.Fatalf("call_indirect caller light/bounded = %t/%t, want true/true", compiled.directPreparedLightAt(0), compiled.directPreparedBoundedAt(0))
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("caller")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if fn.directIntMode != preparedIntCallBlock {
		t.Fatalf("call_indirect direct mode = %d, want prebound call block", fn.directIntMode)
	}
	if !fn.directIntFast || !fn.isolatedFast {
		t.Fatalf("direct/isolated selection = %v/%v, want true/true", fn.directIntFast, fn.isolatedFast)
	}
	if fn.directLeafIntFast {
		t.Fatal("call_indirect caller selected the call-free direct leaf entry")
	}
	if !fn.directTrapIntFast {
		t.Fatal("call_indirect caller did not select the call-free trap-capable entry")
	}
	for _, tc := range []struct {
		idx, want uint64
	}{{0, 13}, {1, 7}, {2, 30}, {3, 9}} {
		got, err := fn.Invoke(tc.idx, 10, 3)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatalf("caller(%d,10,3) = %v, %v; want %d", tc.idx, got, err, tc.want)
		}
	}
	if _, err := fn.Invoke(4, 10, 3); err == nil {
		t.Fatal("out-of-bounds direct prepared call_indirect did not trap")
	}
	if got, err := fn.Invoke(0, 20, 22); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("call after trap = %v, %v; want 42", got, err)
	}
}

func TestPreparedDirectARM64ContinuationTrapRecovery(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      []byte
		wantLight bool
	}{
		{
			name:      "light",
			code:      wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6d, 0x03, 0x40, 0x0b, 0x0b}),
			wantLight: true,
		},
		{
			name: "full-save",
			code: func() []byte {
				body := []byte{
					0x01, 0x01, 0x7f, // one i32 local
					0x20, 0x00, 0x21, 0x02,
					0x20, 0x02, 0x20, 0x01, 0x6d,
					0x03, 0x40, 0x0b, // an empty loop makes scheduler retention ineligible
					0x0b,
				}
				return append(wasmtest.ULEB(uint32(len(body))), body...)
			}(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("div", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(tc.code)),
			)
			compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if !compiled.directPreparedAt(0) || compiled.directPreparedLightAt(0) != tc.wantLight || compiled.directPreparedBoundedAt(0) {
				t.Fatalf("direct/light/bounded selection = %v/%v/%v, want true/%v/false",
					compiled.directPreparedAt(0), compiled.directPreparedLightAt(0), compiled.directPreparedBoundedAt(0), tc.wantLight)
			}
			in, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatalf("instantiate: %v", err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("div")
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if !fn.directIntFast || fn.directIntLight != tc.wantLight || fn.directIntBounded {
				t.Fatalf("prepared direct/light/bounded path = %v/%v/%v, want true/%v/false",
					fn.directIntFast, fn.directIntLight, fn.directIntBounded, tc.wantLight)
			}
			if _, err := fn.Invoke(I32(7), I32(0)); err == nil {
				t.Fatal("division by zero did not unwind through the prepared continuation")
			}
			got, err := fn.Invoke(I32(8), I32(2))
			if err != nil || len(got) != 1 || AsI32(got[0]) != 4 {
				t.Fatalf("post-trap div(8,2) = %v, %v; want [4], nil", got, err)
			}
		})
	}
}

func TestPreparedDirectARM64BoundedEntryRejectsLoop(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) || !compiled.directPreparedLightAt(0) {
		t.Fatalf("loop direct/light selection = %v/%v, want true/true", compiled.directPreparedAt(0), compiled.directPreparedLightAt(0))
	}
	if compiled.directPreparedBoundedAt(0) {
		t.Fatal("loop selected the no-scheduler-release bounded entry")
	}
}

func TestPreparedDirectARM64BoundedEntryRejectsRecursiveCallGraph(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x00, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("recursive function did not retain ordinary scheduler-releasing direct entry")
	}
	if compiled.directPreparedBoundedAt(0) {
		t.Fatal("recursive call graph selected no-scheduler-release bounded entry")
	}
}

func TestPreparedDirectARM64BoundedEntryRejectsInlinedCall(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(1) {
		t.Fatal("inlined caller did not retain ordinary scheduler-releasing direct entry")
	}
	if compiled.directPreparedBoundedAt(1) {
		t.Fatal("caller admitted to bounded entry after inlining")
	}
}

func TestPreparedDirectARM64BoundedEntryRejectsBulkAndTableMutation(t *testing.T) {
	tableSection := func() []byte {
		table := append([]byte{0x70, 0x01}, wasmtest.ULEB(1)...)
		table = append(table, wasmtest.ULEB(65535)...)
		return wasmtest.Section(4, wasmtest.Vec(table))
	}
	tableModule := func(body []byte, sections ...[]byte) []byte {
		moduleSections := [][]byte{
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			tableSection(),
		}
		moduleSections = append(moduleSections, sections...)
		moduleSections = append(moduleSections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		return wasmtest.Module(moduleSections...)
	}

	tests := []struct {
		name       string
		module     []byte
		wantDirect bool
	}{
		{name: "memory.copy", module: benchBulkMemoryModule(0x0a)},
		{name: "memory.fill", module: benchBulkMemoryModule(0x0b)},
		{name: "table.copy", module: tableModule([]byte{0x41, 0x00, 0x41, 0x00, 0x20, 0x00, 0xfc, 0x0e, 0x00, 0x00, 0x0b}), wantDirect: true},
		{name: "table.init", module: tableModule(
			[]byte{0x41, 0x00, 0x41, 0x00, 0x20, 0x00, 0xfc, 0x0c, 0x00, 0x00, 0x0b},
			wasmtest.Section(9, wasmtest.Vec(tableTestPassiveElem(0))),
		), wantDirect: true},
		{name: "table.grow", module: tableModule([]byte{0xd0, 0x70, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x1a, 0x0b}), wantDirect: true},
		{name: "table.fill", module: tableModule([]byte{0x41, 0x00, 0xd0, 0x70, 0x20, 0x00, 0xfc, 0x11, 0x00, 0x0b}), wantDirect: true},
	}
	configs := []struct {
		name   string
		bounds BoundsCheckMode
	}{
		{name: "explicit", bounds: BoundsChecksExplicit},
	}
	if GuardPageSupported() {
		configs = append(configs, struct {
			name   string
			bounds BoundsCheckMode
		}{name: "signals", bounds: BoundsChecksSignalsBased})
	}
	for _, config := range configs {
		for _, tc := range tests {
			t.Run(config.name+"/"+tc.name, func(t *testing.T) {
				compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(config.bounds), tc.module)
				if err != nil {
					t.Fatalf("compile: %v", err)
				}
				if compiled.directPreparedAt(0) != tc.wantDirect {
					t.Fatalf("ordinary direct entry = %v, want %v", compiled.directPreparedAt(0), tc.wantDirect)
				}
				if compiled.directPreparedBoundedAt(0) {
					t.Fatal("bulk or table-mutating function selected the no-scheduler-release bounded entry")
				}
			})
		}
	}
}

func TestPreparedDirectARM64BoundedEntryAllowsFullRegisterSave(t *testing.T) {
	body := []byte{
		0x01, 0x01, 0x7f, // one i32 local
		0x20, 0x00, // local.get 0
		0x21, 0x01, // local.set 1
		0x20, 0x01, // local.get 1
		0x0b,
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) || compiled.directPreparedLightAt(0) || !compiled.directPreparedBoundedAt(0) {
		t.Fatalf("declared-local direct/light/bounded selection = %v/%v/%v, want true/false/true", compiled.directPreparedAt(0), compiled.directPreparedLightAt(0), compiled.directPreparedBoundedAt(0))
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("f")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directIntBounded || fn.directIntLight {
		t.Fatalf("prepared bounded/light selection = %v/%v, want true/false", fn.directIntBounded, fn.directIntLight)
	}
	got, err := fn.Invoke(I32(42))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("f(42) = %v, %v; want [42], nil", got, err)
	}
}

func TestPreparedDirectARM64BoundedEntryAllowsGCProgress(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), benchAddOneModule())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("f")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directIntBounded {
		t.Fatal("test function did not select bounded entry")
	}

	var stop atomic.Bool
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		for !stop.Load() {
			got, err := fn.Invoke(I32(41))
			if err != nil {
				done <- err
				return
			}
			if len(got) != 1 || AsI32(got[0]) != 42 {
				done <- fmt.Errorf("bounded invoke = %v, want [42]", got)
				return
			}
		}
		done <- nil
	}()
	<-started
	for range 8 {
		runtime.GC()
	}
	stop.Store(true)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bounded native entry prevented GC or goroutine progress")
	}
}
