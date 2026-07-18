//go:build ((linux && (amd64 || riscv64)) || arm64) && !tinygo

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// regABITwoResultModule defines a two-integer-result function `swap(a,b)->(b,a)`
// and reaches it three ways so every register-ABI two-result path is exercised:
//   - swap exported directly            → offset-0 adapter stores RAX/RDX (X0/X1) to the buffer
//   - caller(a,b) does `call swap`      → direct reg-ABI call captures the pair
//   - via(idx,a,b) does call_indirect   → tagged internal-entry fast path captures the pair
func regABITwoResultModule() []byte {
	i64 := []wasm.ValType{wasm.I64}
	twoI64 := []wasm.ValType{wasm.I64, wasm.I64}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(twoI64, twoI64),                                      // type0: (i64,i64)->(i64,i64)
			wasmtest.FuncType(append([]wasm.ValType{wasm.I32}, twoI64...), twoI64), // type1: (i32,i64,i64)->(i64,i64)
			wasmtest.FuncType(i64, i64),                                            // type2: unused filler to keep indices explicit
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), // func0 swap:   type0
			wasmtest.ULEB(0), // func1 caller: type0
			wasmtest.ULEB(1), // func2 via:    type1
		)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})), // funcref table, min 1
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("swap", 0, 0),
			wasmtest.ExportEntry("caller", 0, 1),
			wasmtest.ExportEntry("via", 0, 2),
		)),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})), // active elem: table0[0] = func0
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x01, 0x20, 0x00, 0x0b}),                               // swap:   local.get 1; local.get 0; end
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x0b}),                   // caller: local.get 0; local.get 1; call swap; end
			wasmtest.Code([]byte{0x20, 0x01, 0x20, 0x02, 0x20, 0x00, 0x11, 0x00, 0x00, 0x0b}), // via: a; b; idx; call_indirect type0 table0; end
		)),
	)
}

func TestRegABITwoIntResults(t *testing.T) {
	in, err := Instantiate(MustCompile(regABITwoResultModule()), InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	const a, b = uint64(0x1111222233334444), uint64(0x5566778899aabbcc)
	want := []uint64{b, a} // swap returns (b, a)

	for _, tc := range []struct {
		name string
		args []uint64
	}{
		{"swap", []uint64{a, b}},   // adapter path (export → offset-0 adapter)
		{"caller", []uint64{a, b}}, // direct reg-ABI call to swap, pair captured
		{"via", []uint64{0, a, b}}, // call_indirect through the tagged internal entry
	} {
		got, err := in.Invoke(tc.name, tc.args...)
		if err != nil {
			t.Fatalf("Invoke %s: %v", tc.name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s(%#x) = %#x, want %#x", tc.name, tc.args, got, want)
		}
	}
}
