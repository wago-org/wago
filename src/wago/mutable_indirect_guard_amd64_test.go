//go:build linux && amd64 && !tinygo

package wago

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The first call_indirect is at body PC 7 of global function 0. The opt-in
// guard specification is 0:7:1:0 (caller:pc:target:table).
func mutableGuardOracleModule() []byte {
	i32 := []wasm.ValType{wasm.I32}
	f64 := []wasm.ValType{wasm.F64}
	set := func(ref []byte) []byte {
		return wasmtest.Code(tableTestBody(tableTestI32Const(0), ref, []byte{0x26, 0x00}))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.F64, wasm.I32}, f64),
			wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.I32}, f64),
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, i32),
		)),
		tableTestFuncSection(0, 1, 1, 2, 2, 2, 3, 2),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x03, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("call", 0, 0),
			wasmtest.ExportEntry("setOriginal", 0, 3),
			wasmtest.ExportEntry("setAlt", 0, 4),
			wasmtest.ExportEntry("clear", 0, 5),
			wasmtest.ExportEntry("setWrong", 0, 7),
			wasmtest.ExportEntry("table", 1, 0),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 1, 2, 6))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestLocalGet(1), tableTestLocalGet(2), tableTestLocalGet(0), tableTestCallIndirect(1, 0))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestLocalGet(1), []byte{0xb7, 0xa0})),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestLocalGet(1), []byte{0xb7, 0xa1})),
			set(tableTestRefFunc(1)),
			set(tableTestRefFunc(2)),
			set(tableTestRefNullFunc()),
			wasmtest.Code(tableTestBody(tableTestI32Const(7))),
			set(tableTestRefFunc(6)),
		)),
	)
}

func TestGuardedMutableIndirectCall(t *testing.T) {
	c := MustCompile(mutableGuardOracleModule())
	defer c.Close()
	t.Logf("native code bytes: %d", c.CodeSize())
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	call := func(idx uint64, a float64, b uint64, want float64) {
		t.Helper()
		got, err := in.Invoke("call", idx, math.Float64bits(a), b)
		if err != nil || len(got) != 1 || got[0] != math.Float64bits(want) {
			t.Fatalf("call(%d,%g,%d)=%v,%v; want %g", idx, a, b, got, err, want)
		}
	}
	set := func(name string) {
		t.Helper()
		if _, err := in.Invoke(name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	call(0, 12, 5, 17) // initial guarded target
	call(1, 12, 5, 7)  // another local target
	set("setAlt")
	call(0, 12, 5, 7) // mutable miss
	set("setOriginal")
	call(0, 12, 5, 17) // guarded target restored
	set("clear")
	if _, err := in.Invoke("call", 0, math.Float64bits(12), 5); err == nil {
		t.Fatal("null table entry did not trap")
	}
	set("setWrong")
	if _, err := in.Invoke("call", 0, math.Float64bits(12), 5); err == nil {
		t.Fatal("wrong type did not trap")
	}
	if _, err := in.Invoke("call", 3, math.Float64bits(12), 5); err == nil {
		t.Fatal("out-of-bounds index did not trap")
	}
}
