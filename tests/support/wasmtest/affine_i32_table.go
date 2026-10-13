package wasmtest

import "github.com/wago-org/wago/src/core/compiler/wasm"

// AffineI32Table creates distinct local leaves x*scale+bias, reached by a runtime
// table index. loadArgument evaluates an i32 load before evaluating that index.
func AffineI32Table(scale uint32, biases []uint32, minimum uint32, exportTable, loadArgument bool) []byte {
	i32 := []wasm.ValType{wasm.I32}
	i64 := []wasm.ValType{wasm.I64}
	args := []wasm.ValType{wasm.I32, wasm.I32}
	types := Vec(FuncType(i32, i32), FuncType(args, i32), FuncType(i64, i64), FuncType(args, i64))
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
	// Keep a distinct wide value below the call operands.
	prefix := append([]byte{0x42}, SLEB64(0x1122334455667788)...)
	prefix = append(prefix, caller[:len(caller)-1]...)
	prefix = append(prefix, 0xad, 0x7c, 0x0b)
	funcs = append(funcs, []byte{3})
	bodies = append(bodies, Code(prefix))
	exports = append(exports, ExportEntry("prefix", 0, uint32(len(biases)+1)))
	// This valid call requests an incompatible numeric signature.
	funcs = append(funcs, []byte{1})
	bodies = append(bodies, Code([]byte{0x20, 0, 0xad, 0x20, 1, 0x11, 2, 0, 0xa7, 0x0b}))
	exports = append(exports, ExportEntry("wrong", 0, uint32(len(biases)+2)))
	// Repeated calls keep a dirty accumulator and counter live across the site.
	loop := []byte{2, 1, 0x7f, 1, 0x7e, 3, 0x40, 0x20, 3, 0x20, 0, 0x20, 2, 0x6a, 0x20, 2, 0x41}
	loop = append(loop, SLEB64(int64(minimum))...)
	loop = append(loop, 0x70, 0x11, 0, 0, 0xad, 0x7c, 0x21, 3, 0x20, 2, 0x41, 1, 0x6a, 0x22, 2, 0x20, 1, 0x49, 0x0d, 0, 0x0b, 0x20, 3, 0x0b)
	funcs = append(funcs, []byte{3})
	bodies = append(bodies, append(ULEB(uint32(len(loop))), loop...))
	exports = append(exports, ExportEntry("loop", 0, uint32(len(biases)+3)))
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
