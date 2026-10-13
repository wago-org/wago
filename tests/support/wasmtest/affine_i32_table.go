package wasmtest

import "github.com/wago-org/wago/src/core/compiler/wasm"

// AffineI32Table creates distinct local leaves x*scale+bias, reached by a runtime
// table index. loadArgument evaluates an i32 load before evaluating that index.
func AffineI32Table(scale uint32, biases []uint32, minimum uint32, exportTable, loadArgument bool) []byte {
	i32 := []wasm.ValType{wasm.I32}
	types := Vec(FuncType(i32, i32), FuncType([]wasm.ValType{wasm.I32, wasm.I32}, i32))
	funcs := make([][]byte, 0, len(biases)+1)
	bodies := make([][]byte, 0, len(biases)+1)
	elem := []byte{0, 0x41, 0, 0x0b}
	elem = append(elem, ULEB(uint32(len(biases)))...)
	for i, bias := range biases {
		funcs = append(funcs, []byte{0})
		elem = append(elem, ULEB(uint32(i))...)
		b := []byte{0x20, 0, 0x41}
		b = append(b, SLEB64(int64(int32(scale)))...)
		b = append(b, 0x6c, 0x41)
		b = append(b, SLEB64(int64(int32(bias)))...)
		b = append(b, 0x6a, 0x0b)
		bodies = append(bodies, Code(b))
	}
	funcs = append(funcs, []byte{1})
	caller := []byte{0x20, 0}
	if loadArgument {
		caller = append(caller, 0x28, 2, 0)
	}
	caller = append(caller, 0x20, 1, 0x11, 0, 0, 0x0b)
	bodies = append(bodies, Code(caller))
	exports := [][]byte{ExportEntry("call", 0, uint32(len(biases)))}
	if exportTable {
		exports = append(exports, ExportEntry("table", 1, 0))
	}
	sections := [][]byte{Section(1, types), Section(3, Vec(funcs...)), Section(4, Vec(append([]byte{0x70, 0}, ULEB(minimum)...)))}
	if loadArgument {
		sections = append(sections, Section(5, Vec([]byte{0, 1})))
		exports = append(exports, ExportEntry("memory", 2, 0))
	}
	sections = append(sections, Section(7, Vec(exports...)), Section(9, Vec(elem)), Section(10, Vec(bodies...)))
	return Module(sections...)
}
