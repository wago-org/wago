package wasmtest

import "github.com/wago-org/wago/src/core/compiler/wasm"

// ClosedReferenceBareIndirectTails uses only nullable untyped private references
// and numeric tails. Function0's actual type4 equals declared type0; table row2
// names function3/type5. The cycle revisits the private source with changed arity.
func ClosedReferenceBareIndirectTails(vector bool) []byte {
	types := [][]byte{FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		FuncType([]wasm.ValType{wasm.FuncRef, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		FuncType([]wasm.ValType{wasm.F32}, []wasm.ValType{wasm.I32}),
		FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32})}
	a := []byte{0x20, 0, 0x45, 0x04, 0x7f, 0x41, 42, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x42}
	a = append(a, SLEB64(-0x123456789abcdef)...)
	a = append(a, 0x41, 2, 0x13, 5, 0, 0x0b, 0x0b)
	b := []byte{0x20, 1, 0x42}
	b = append(b, SLEB64(-0x123456789abcdef)...)
	b = append(b, 0x52, 0x04, 0x40, 0, 0x0b, 0xd2, 0, 0x20, 0, 0x41, 0, 0x12, 1, 0x0b)
	functions := []byte{5, 4, 1, 2, 5, 3}
	codes := [][]byte{Code(a), Code([]byte{0x20, 1, 0x20, 2, 0x13, 0, 0, 0x0b}),
		Code([]byte{0xd2, 0, 0x20, 1, 0x20, 0, 0x10, 1, 0x0b}), Code(b), Code([]byte{0x41, 43, 0x0b})}
	if vector {
		types = append(types, FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))
		functions[0]++
		functions = append(functions, 6)
		codes = append(codes, Code([]byte{0x20, 0, 0x0b}))
	}
	return Module(Section(1, Vec(types...)), Section(3, functions), Section(4, []byte{1, 0x70, 1, 5, 5}),
		Section(7, Vec(ExportEntry("run", 0, 2), ExportEntry("safe", 0, 0))),
		Section(9, []byte{2, 0, 0x41, 0, 0x0b, 1, 0, 0, 0x41, 2, 0x0b, 3, 3, 4, 0}), Section(10, Vec(codes...)))
}
