//go:build linux && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// workerResetModule has a fixed context for every fresh/reused pair. All
// functions take and return i64. Each module has one page of memory.
func workerResetModule(width int) []byte {
	largeBody := []byte{}
	for i := 1; i <= 160; i++ {
		// local[i] = argument + i; then sum all 160 values from a deep stack.
		largeBody = append(largeBody, 0x20, 0, 0x42)
		largeBody = append(largeBody, wasmtest.SLEB64(int64(i))...)
		largeBody = append(largeBody, 0x7c, 0x21)
		largeBody = append(largeBody, wasmtest.ULEB(uint32(i))...)
	}
	for i := 1; i <= 160; i++ {
		largeBody = append(largeBody, 0x20)
		largeBody = append(largeBody, wasmtest.ULEB(uint32(i))...)
	}
	for i := 1; i < 160; i++ {
		largeBody = append(largeBody, 0x7c)
	}
	largeBody = append(largeBody, 0x0b)
	// Float/vector locals force the large fixture to use fallback lowering.
	large := append([]byte{3, 0xa0, 1, 0x7e, 16, 0x7c, 16, 0x7b}, largeBody...)
	scalar := append([]byte{1, 0xa0, 1, 0x7e}, largeBody...)

	// (block (result i64) (i64.const 23) (br_table 0 ... 0 (i32.wrap_i64 ...)))
	branches := []byte{0, 0x02, 0x7e, 0x42, 23, 0x20, 0, 0xa7, 0x0e}
	branches = append(branches, wasmtest.ULEB(257)...)
	branches = append(branches, make([]byte, 258)...)
	branches = append(branches, 0x0b, 0x0b)
	control := []byte{0}
	for i := 0; i < 48; i++ {
		control = append(control, 0x02, 0x7e)
	}
	control = append(control, 0x20, 0)
	for i := 0; i < 49; i++ {
		control = append(control, 0x0b)
	}
	addr, flags := byte(0x41), byte(0) // i32.const and memory32
	if width == 64 {
		addr, flags = 0x42, 4
	} // i64.const and memory64
	bodies := [][]byte{
		{0, 0x20, 0, 0x42, 7, 0x7c, 0x0b}, // argument + 7
		large,
		branches,
		// f64.convert_i64_u; f64.const 1.25; f64.add; i64.reinterpret_f64
		{0, 0x20, 0, 0xba, 0x44, 0, 0, 0, 0, 0, 0, 0xf4, 0x3f, 0xa0, 0xbd, 0x0b},
		// i64x2.splat twice; i64x2.add; i64x2.extract_lane 0
		{0, 0x20, 0, 0xfd, 0x12, 0x20, 0, 0xfd, 0x12, 0xfd, 0xce, 1, 0xfd, 0x1d, 0, 0x0b},
		{0, addr, 0, 0x20, 0, 0x37, 3, 0, addr, 0, 0x29, 3, 0, 0x0b}, // store/load i64
		{0, 0x42, 0xe4, 0, 0x20, 0, 0x80, 0x0b},                      // 100 / argument, unsigned
		{0, 0x20, 0, 0x10, 0, 0x42, 3, 0x7e, 0x0b},                   // call tiny, then multiply by 3
		{3, 1, 0x7f, 1, 0x7c, 1, 0x7e, 0x20, 0, 0x42, 13, 0x85, 0x21, 3, 0x20, 3, 0x0b},
		control,
		scalar,
	}
	var functions, exports, codes [][]byte
	for i, body := range bodies {
		functions = append(functions, []byte{0})
		exports = append(exports, wasmtest.ExportEntry(workerResetNames[i], 0, uint32(i)))
		codes = append(codes, append(wasmtest.ULEB(uint32(len(body))), body...))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(functions...)),
		wasmtest.Section(5, wasmtest.Vec([]byte{flags, 1})),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}
