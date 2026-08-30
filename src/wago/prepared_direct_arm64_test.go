//go:build arm64 && !tinygo && (linux || darwin || windows)

package wago

import (
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestPreparedDirectARM64IgnoresUnusedModuleMemory(t *testing.T) {
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
	if compiled.directPreparedAt(1) {
		t.Fatal("memory.size function selected the ARM64 direct prepared entry")
	}
	if compiled.directPreparedAt(2) {
		t.Fatal("function calling memory.size selected the ARM64 direct prepared entry")
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("add")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directIntFast {
		t.Fatal("memory-independent function did not prepare the ARM64 direct integer entry")
	}
	got, err := fn.Invoke2(20, 22)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("add(20,22) = %v, %v; want 42", got, err)
	}
}

func TestPreparedDirectARM64CallIndirectAndTrapRecovery(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), callIndirectModule(2, 1, 2))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("call_indirect caller did not select the ARM64 direct prepared entry")
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("caller")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directIntFast || fn.isolatedFast {
		t.Fatalf("direct/private selection = %v/%v, want true/false", fn.directIntFast, fn.isolatedFast)
	}
	if fn.directLeafIntFast {
		t.Fatal("call_indirect caller selected the call-free direct leaf entry")
	}
	for _, tc := range []struct {
		idx, want uint64
	}{{0, 13}, {1, 7}} {
		got, err := fn.Invoke(tc.idx, 10, 3)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatalf("caller(%d,10,3) = %v, %v; want %d", tc.idx, got, err, tc.want)
		}
	}
	if _, err := fn.Invoke(2, 10, 3); err == nil {
		t.Fatal("out-of-bounds direct prepared call_indirect did not trap")
	}
	if got, err := fn.Invoke(0, 20, 22); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("call after trap = %v, %v; want 42", got, err)
	}
}

func TestPreparedDirectARM64BranchTable(t *testing.T) {
	body := []byte{
		0x02, 0x40, 0x02, 0x40, 0x02, 0x40, 0x02, 0x40,
		0x20, 0x00, 0x0e, 0x03, 0x00, 0x01, 0x02, 0x03,
		0x0b, 0x41, 0x0a, 0x0f,
		0x0b, 0x41, 0x14, 0x0f,
		0x0b, 0x41, 0x1e, 0x0f,
		0x0b, 0x41, 0x28, 0x0b,
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("classify", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("branch-table function did not select the ARM64 direct prepared entry")
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("classify")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !fn.directLeafIntFast {
		t.Fatal("branch-table leaf did not prepare the ARM64 direct leaf entry")
	}
	for _, tc := range []struct {
		selector, want uint64
	}{{0, 10}, {1, 20}, {2, 30}, {3, 40}, {100, 40}} {
		got, err := fn.Invoke1(tc.selector)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatalf("classify(%d) = %v, %v; want %d", tc.selector, got, err, tc.want)
		}
	}
}

func TestPreparedDirectARM64PureIntegerLeaf(t *testing.T) {
	body := make([]byte, 0, 48)
	term := func(shift int64, mask int64) {
		body = append(body, 0x20, 0x00)
		if shift != 0 {
			body = append(body, 0x42)
			body = append(body, wasmtest.SLEB64(shift)...)
			body = append(body, 0x88)
		}
		body = append(body, 0x42)
		body = append(body, wasmtest.SLEB64(mask)...)
		body = append(body, 0x83)
	}
	term(24, 0xff000000)
	term(16, 0x00ff0000)
	term(0, 0x000000ff)
	term(8, 0x0000ff00)
	body = append(body, 0x84, 0x84, 0x84, 0xa7, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("pack", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("pure integer leaf did not select the ARM64 direct prepared entry")
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("pack")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	for _, x := range []uint64{0, 0x12345678, 0x0044004300420041, ^uint64(0)} {
		want := uint32(x>>24&0xff000000 | x>>16&0x00ff0000 | x&0x000000ff | x>>8&0x0000ff00)
		got, err := fn.Invoke1(x)
		if err != nil || len(got) != 1 || got[0] != uint64(want) {
			t.Fatalf("pack(%#x) = %v, %v; want %#x", x, got, err, want)
		}
	}
}

func TestPreparedDirectARM64UnsignedMultiplyHigh(t *testing.T) {
	body := []byte{
		0x20, 0x00, 0x42, 0x20, 0x88, 0x22, 0x02,
		0x20, 0x01, 0x42, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x83, 0x22, 0x03, 0x7e,
		0x20, 0x00, 0x42, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x83, 0x22, 0x00,
		0x20, 0x03, 0x7e, 0x42, 0x20, 0x88, 0x7c, 0x21, 0x03,
		0x20, 0x01, 0x42, 0x20, 0x88, 0x22, 0x01, 0x20, 0x02, 0x7e,
		0x20, 0x03, 0x42, 0x20, 0x88, 0x7c,
		0x20, 0x00, 0x20, 0x01, 0x7e,
		0x20, 0x03, 0x42, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x83, 0x7c,
		0x42, 0x20, 0x88, 0x7c, 0x0b,
	}
	function := append([]byte{0x01, 0x02, 0x7e}, body...) // two declared i64 locals
	code := append(wasmtest.ULEB(uint32(len(function))), function...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("mulhi", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.directPreparedAt(0) {
		t.Fatal("multiply-high leaf did not select the ARM64 direct prepared entry")
	}
	if len(compiled.code) > 64 {
		t.Fatalf("multiply-high native code = %d bytes, want single-instruction specialization", len(compiled.code))
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("mulhi")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	for _, tc := range [][2]uint64{{0, 0}, {1, 1}, {^uint64(0), ^uint64(0)}, {0x123456789abcdef0, 0xfedcba9876543210}} {
		want, _ := bits.Mul64(tc[0], tc[1])
		got, err := fn.Invoke2(tc[0], tc[1])
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("mulhi(%#x,%#x) = %v, %v; want %#x", tc[0], tc[1], got, err, want)
		}
	}
}
